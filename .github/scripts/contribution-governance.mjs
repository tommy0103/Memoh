import { bodyFingerprint, ciWorkflows, classify, typeLabels, validate } from './contribution-policy.mjs';

const marker = '<!-- memoh-contribution-format:v1 -->';
const statusContext = 'PR Format';
const pathFor = name => `.github/workflows/${name}`;

export function belongsToPR(run, pr) {
  return run.event === 'pull_request' && ciWorkflows.some(name => run.path === pathFor(name))
    && run.head_sha === pr.head.sha
    && (run.pull_requests?.some(item => item.number === pr.number && item.head.sha === pr.head.sha && item.base.repo.id === pr.base.repo.id)
      || (!run.pull_requests?.length && run.head_repository?.id === pr.head.repo?.id && run.head_branch === pr.head.ref));
}

export async function syncLabels(github, repo, issue, wanted, managed) {
  const current = issue.labels.map(label => typeof label === 'string' ? label : label.name);
  const missing = wanted.filter(label => !current.includes(label));
  if (missing.length) await github.rest.issues.addLabels({ ...repo, issue_number: issue.number, labels: missing });
  for (const name of current.filter(label => managed(label) && !wanted.includes(label))) {
    try { await github.rest.issues.removeLabel({ ...repo, issue_number: issue.number, name }); }
    catch (error) { if (error.status !== 404) throw error; }
  }
}

async function samePR(github, repo, pr) {
  const { data: fresh } = await github.rest.pulls.get({ ...repo, pull_number: pr.number });
  return fresh.state === 'open' && bodyFingerprint(fresh) === bodyFingerprint(pr);
}

export async function reconcileRuns(github, repo, pr, core) {
  const runs = await github.paginate(github.rest.actions.listWorkflowRunsForRepo, {
    ...repo, event: 'pull_request', head_sha: pr.head.sha, per_page: 100,
  });
  // Only the latest attempt/run of each workflow is relevant; never resurrect obsolete runs.
  const latest = new Map();
  for (const run of runs.filter(run => belongsToPR(run, pr)).sort((a, b) => b.id - a.id)) {
    if (!latest.has(run.path)) latest.set(run.path, run);
  }
  for (const run of latest.values()) {
    if (!await samePR(github, repo, pr)) return;
    if (run.conclusion === 'action_required') {
      await github.rest.actions.approveWorkflowRun({ ...repo, run_id: run.id });
      core.info(`Approved PR #${pr.number} run ${run.id}`);
    }
  }
}

async function publishComment(github, repo, issue, errors) {
  const comments = await github.paginate(github.rest.issues.listComments, { ...repo, issue_number: issue.number, per_page: 100 });
  const previous = comments.find(comment => comment.user?.login === 'github-actions[bot]' && comment.body?.startsWith(marker));
  if (!errors.length && !previous) return;
  const body = errors.length
    ? `${marker}\n@${issue.user.login} Please complete the following information using the template:\n\n${errors.map(error => `- ${error}`).join('\n')}\n\nEditing the description triggers another check; format feedback does not block or cancel code CI.`
    : `${marker}\nFormat check passed; removed \`needs:format\`.`;
  if (previous?.body === body) return;
  if (previous) await github.rest.issues.updateComment({ ...repo, comment_id: previous.id, body });
  else await github.rest.issues.createComment({ ...repo, issue_number: issue.number, body });
}

export async function inspectPR({ github, context, core }, number, { classifyChanges = true } = {}) {
  const repo = context.repo;
  const { data: pr } = await github.rest.pulls.get({ ...repo, pull_number: number });
  if (pr.state !== 'open') return;
  const { errors, type } = validate(pr.body, true);
  const wanted = [...(type ? [type] : []), ...(errors.length ? ['needs:format'] : [])];
  let classification;
  let classificationError;
  if (classifyChanges) {
    try {
      const files = await github.paginate(github.rest.pulls.listFiles, { ...repo, pull_number: number, per_page: 100 });
      if (files.length !== pr.changed_files) throw new Error(`Incomplete PR file list: ${files.length}/${pr.changed_files}`);
      classification = classify(files);
      wanted.push(classification.size, ...classification.changes);
    } catch (error) { classificationError = error; }
  }
  if (!await samePR(github, repo, pr)) return;
  const statuses = await github.paginate(github.rest.repos.listCommitStatusesForRef, { ...repo, ref: pr.head.sha, per_page: 100 });
  const last = statuses.find(status => status.context === statusContext);
  // Report format issues through labels and comments without blocking code checks or workflow approval.
  const state = 'success';
  const description = `${bodyFingerprint(pr)} ${errors.length ? 'Format corrections needed (non-blocking)' : 'Format check passed'}`;
  if (last?.state !== state || last?.description !== description) {
    await github.rest.repos.createCommitStatus({ ...repo, sha: pr.head.sha, state, context: statusContext, description,
      target_url: `${context.serverUrl}/${repo.owner}/${repo.repo}/actions/runs/${context.runId}` });
  }
  // Retire only the latest bot cancellation status on the current head; preserve real tests and other accounts' statuses.
  const seen = new Set();
  for (const status of statuses) {
    if (seen.has(status.context)) continue;
    seen.add(status.context);
    if (/^PR Format cancellation \/ \d+$/.test(status.context) && status.state === 'failure' && status.creator?.login === 'github-actions[bot]') {
      await github.rest.repos.createCommitStatus({ ...repo, sha: pr.head.sha, state: 'success', context: status.context, description: 'Legacy format cancellation retired; does not indicate code checks passed' });
    }
  }
  await syncLabels(github, repo, pr, wanted, name => typeLabels.includes(name) || name === 'needs:format'
    || (classification && (name.startsWith('size:') || name.startsWith('change:'))));
  await publishComment(github, repo, pr, errors);
  await reconcileRuns(github, repo, pr, core);
  if (classifyChanges) {
    await core.summary.addHeading(`PR #${number}`).addRaw(`Format: ${errors.length ? errors.join('; ') : 'Passed'}\n\nOriginal additions/deletions: ${pr.additions}/${pr.deletions}\n\n`)
      .addRaw(classification ? `Filtered additions/deletions: ${classification.additions}/${classification.deletions}; Excluded ${classification.ignored} files; ${classification.size}; ${classification.changes.join(', ')}\n` : 'Classification failed; existing size/change labels were preserved.\n').write();
  }
  if (classificationError) throw classificationError;
}

export async function run({ github, context, core }) {
  const args = { github, context, core };
  const payload = context.payload;
  if (context.eventName === 'issues') {
    const { data: issue } = await github.rest.issues.get({ ...context.repo, issue_number: payload.issue.number });
    if (issue.state !== 'open') return;
    const { errors, type } = validate(issue.body, false);
    await syncLabels(github, context.repo, issue, [...(type ? [type] : []), ...(errors.length ? ['needs:format'] : [])], name => typeLabels.includes(name) || name === 'needs:format');
    await publishComment(github, context.repo, issue, errors);
    return;
  }
  if (payload.pull_request) return inspectPR(args, payload.pull_request.number);
  if (payload.inputs?.pr_number) return inspectPR(args, Number(payload.inputs.pr_number));
  if (payload.workflow_run?.event === 'pull_request' && payload.workflow_run.pull_requests?.length) {
    for (const pr of payload.workflow_run.pull_requests) await inspectPR(args, pr.number, { classifyChanges: false });
    return;
  }
  // Scheduled reconciliation also covers PRs created by GITHUB_TOKEN, whose events do not start workflows.
  const commits = await github.paginate(github.rest.repos.listCommits, { ...context.repo, path: '.github/workflows/contribution-governance.yml', per_page: 100 });
  const started = commits.at(-1)?.commit.committer.date;
  if (!started) throw new Error('Cannot determine governance activation date');
  const prs = await github.paginate(github.rest.pulls.list, { ...context.repo, state: 'open', per_page: 100 });
  for (const pr of prs) {
    if (pr.created_at < started) {
      const statuses = await github.paginate(github.rest.repos.listCommitStatusesForRef, { ...context.repo, ref: pr.head.sha, per_page: 100 });
      if (!statuses.some(status => status.context === statusContext && status.creator?.login === 'github-actions[bot]')) continue;
    }
    try { await inspectPR(args, pr.number); }
    catch (error) { core.setFailed(`PR #${pr.number}: ${error.message}`); }
  }
}

// Older PR branches still call this entry point through a reusable workflow; keep it as a no-op.
export async function gate({ core }) {
  core.info('Format checks now provide independent feedback; code CI does not wait for description validation.');
}
