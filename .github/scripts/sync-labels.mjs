import { readFile } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';

export async function readLabels() {
  const labels = JSON.parse(await readFile(new URL('../labels.json', import.meta.url), 'utf8'));
  const names = new Set();
  for (const label of labels) {
    if (!label.name || names.has(label.name.toLowerCase()) || !/^[0-9a-f]{6}$/i.test(label.color) || typeof label.description !== 'string') throw new Error('Invalid or duplicate label definition');
    names.add(label.name.toLowerCase());
  }
  return labels;
}
export async function sync(github, repo) {
  const desired = await readLabels();
  const current = await github.paginate(github.rest.issues.listLabelsForRepo, { ...repo, per_page: 100 });
  for (const label of desired) {
    const previous = current.find(item => item.name.toLowerCase() === label.name.toLowerCase());
    if (!previous) await github.rest.issues.createLabel({ ...repo, ...label });
    else if (previous.name !== label.name || previous.color.toLowerCase() !== label.color.toLowerCase() || previous.description !== label.description) {
      await github.rest.issues.updateLabel({ ...repo, name: previous.name, new_name: label.name, color: label.color, description: label.description });
    }
  }
}
// CLI is additive/update-only. Destructive historical migration is separate and explicitly invoked.
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const repo = process.argv[2];
  if (!repo || !/^[\w.-]+\/[\w.-]+$/.test(repo)) throw new Error('Usage: node .github/scripts/sync-labels.mjs OWNER/REPO [--apply]');
  const desired = await readLabels();
  if (!process.argv.includes('--apply')) console.log(JSON.stringify(desired, null, 2));
  else for (const label of desired) {
    execFileSync('gh', ['label', 'create', label.name, '--repo', repo, '--color', label.color, '--description', label.description, '--force'], { stdio: 'inherit' });
  }
}
