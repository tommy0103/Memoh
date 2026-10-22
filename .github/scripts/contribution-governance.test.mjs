import assert from 'node:assert/strict';
import test from 'node:test';
import { belongsToPR, gate, inspectPR, reconcileRuns, run, syncLabels } from './contribution-governance.mjs';
import { bodyFingerprint } from './contribution-policy.mjs';

const body = `## Author\n- [x] Agent\n## Type\n- [x] bug\n## Summary\nFix the reported issue\n## Validation\nRegression tests passed\n## Screenshots / Recordings\nBackend-only change without a UI; verified using API requests\n## Human QA\n- [ ] Human QA passed`;
const pr = { number:1,state:'open',head:{sha:'abc',repo:{id:2},ref:'patch'},base:{ref:'main',repo:{id:1}},body,labels:[],user:{login:'author'},changed_files:1,additions:10,deletions:0 };
const ci = {id:10,path:'.github/workflows/eslint.yml',head_sha:'abc',event:'pull_request',run_attempt:1,pull_requests:[{number:1,head:{sha:'abc'},base:{repo:{id:1}}}]};
function mock({ runs=[], jobs=[], fresh=pr, statuses=[], files=[{filename:'apps/web/a.vue',additions:10,deletions:0}], comments=[] } = {}) {
  const calls=[];
  const rest={actions:{},pulls:{},repos:{},issues:{}};
  for (const [section,names] of Object.entries({actions:['approveWorkflowRun','cancelWorkflowRun','reRunWorkflow'],repos:['createCommitStatus'],issues:['addLabels','removeLabel','createComment','updateComment']})) {
    for (const name of names) rest[section][name]=async args=>{calls.push({name,args});return {data:{}};};
  }
  rest.pulls.get=async()=>({data:fresh});
  for (const [section,names] of Object.entries({actions:['listWorkflowRunsForRepo','listJobsForWorkflowRun'],repos:['listCommitStatusesForRef'],pulls:['listFiles'],issues:['listComments']})) for (const name of names) rest[section][name]=name;
  const values={listWorkflowRunsForRepo:runs,listJobsForWorkflowRun:jobs,listCommitStatusesForRef:statuses,listFiles:files,listComments:comments};
  const github={rest,paginate:async method=>values[method]};
  const summary={addHeading(){return this;},addRaw(){return this;},async write(){}};
  return {github,calls,core:{info(){},summary},context:{repo:{owner:'o',repo:'r'},serverUrl:'https://github.com',runId:99}};
}
test('approval binds the repository, PR, workflow path and current head', () => {
  assert.equal(belongsToPR(ci,pr),true);
  for (const changed of [{...ci,head_sha:'old'},{...ci,event:'push'},{...ci,path:'.github/workflows/release.yml'},{...ci,pull_requests:[{number:99,head:{sha:'abc'},base:{repo:{id:1}}}]}]) assert.equal(belongsToPR(changed,pr),false);
  assert.equal(belongsToPR({...ci,pull_requests:[],head_repository:{id:2},head_branch:'patch'},pr),true);
  assert.equal(belongsToPR({...ci,pull_requests:[],head_repository:{id:3},head_branch:'patch'},pr),false);
});
test('approval depends on the current PR, not its description format', async () => {
  for (const [fresh,count] of [[pr,1],[{...pr,body:'changed'},0]]) {
    const m=mock({runs:[{...ci,status:'completed',conclusion:'action_required'}],fresh});
    await reconcileRuns(m.github,m.context.repo,pr,m.core);
    assert.equal(m.calls.filter(c=>c.name==='approveWorkflowRun').length,count);
  }
});
test('only latest run is approved, never obsolete runs of the same workflow', async () => {
  const m=mock({runs:[{...ci,id:9,conclusion:'action_required'},{...ci,id:10,status:'in_progress'}]});
  await reconcileRuns(m.github,m.context.repo,pr,m.core);
  assert.equal(m.calls.length,0);
});
test('invalid descriptions never cancel or rerun CI, but still approve eligible runs', async () => {
  for (const run of [
    {...ci,status:'in_progress'},
    {...ci,status:'completed',conclusion:'cancelled'},
    {...ci,status:'completed',conclusion:'failure'},
    {...ci,status:'completed',conclusion:'action_required'},
  ]) {
    const invalid = {...pr,body:body.replace('- [x] Agent','- [x] Agent\n- [x] Human')};
    const m=mock({runs:[run],fresh:invalid});
    await inspectPR(m,1);
    assert.ok(!m.calls.some(c=>['cancelWorkflowRun','reRunWorkflow'].includes(c.name)));
    assert.equal(m.calls.filter(c=>c.name==='approveWorkflowRun').length,run.conclusion==='action_required'?1:0);
    assert.equal(m.calls.find(c=>c.name==='createCommitStatus').args.state,'success');
    assert.ok(m.calls.some(c=>c.name==='addLabels' && c.args.labels.includes('needs:format')));
    assert.ok(m.calls.find(c=>c.name==='createComment').args.body.includes('Author'));
  }
});
test('legacy cancellation cleanup only retires the latest bot-owned failed status',async()=>{
  const context='PR Format cancellation / 10';
  const failed={context,state:'failure',creator:{login:'github-actions[bot]'}};
  for (const [statuses,count] of [
    [[failed],1],
    [[{...failed,state:'success'},failed],0],
    [[{...failed,creator:{login:'person'}},failed],0],
    [[{...failed,context:'Test'}],0],
  ]) {
    const m=mock({statuses});
    await inspectPR(m,1);
    const cleanup=m.calls.filter(c=>c.name==='createCommitStatus' && c.args.context===context);
    assert.equal(cleanup.length,count);
    if(count) assert.equal(cleanup[0].args.state,'success');
    assert.ok(!m.calls.some(c=>c.name==='reRunWorkflow'));
  }
});
test('synchronization removes obsolete managed labels and preserves unrelated labels',async()=>{
  const m=mock();
  await syncLabels(m.github,m.context.repo,{number:1,labels:[{name:'size:XS'},{name:'custom'}]},['size:M'],name=>name.startsWith('size:'));
  assert.deepEqual(m.calls.map(c=>c.name),['addLabels','removeLabel']);
  assert.equal(m.calls[1].args.name,'size:XS');
});
test('classification failure does not invent a format error or remove prior scope labels',async()=>{
  const m=mock({files:[],fresh:{...pr,labels:[{name:'size:L'}]}});
  await assert.rejects(inspectPR(m,1),/Incomplete/);
  assert.equal(m.calls.find(c=>c.name==='createCommitStatus').args.state,'success');
  assert.ok(!m.calls.some(c=>c.name==='removeLabel' && c.args.name==='size:L'));
  assert.ok(!m.calls.some(c=>c.name==='addLabels' && c.args.labels.includes('needs:format')));
});
test('fixed descriptions update the existing bot comment instead of posting another',async()=>{
  const m=mock({comments:[{id:5,user:{login:'github-actions[bot]'},body:'<!-- memoh-contribution-format:v1 -->\nPrevious error'}]});
  await inspectPR(m,1);
  assert.equal(m.calls.filter(c=>c.name==='updateComment').length,1);
  assert.equal(m.calls.filter(c=>c.name==='createComment').length,0);
});
test('unchanged success is idempotent',async()=>{
  const m=mock({fresh:{...pr,labels:[{name:'bug'},{name:'size:XS'},{name:'change:web'}]},statuses:[{context:'PR Format',state:'success',description:`${bodyFingerprint(pr)} Format check passed`}]});
  await inspectPR(m,1);
  assert.deepEqual(m.calls,[]);
});
test('issues use current API body, not stale event body',async()=>{
  const m=mock();
  m.context.eventName='issues';m.context.payload={issue:{number:1,body:'stale'}};
  m.github.rest.issues.get=async()=>({data:{number:1,state:'open',body:'',labels:[],user:{login:'person'}}});
  await run(m);
  assert.ok(m.calls.find(c=>c.name==='createComment').args.body.includes('@person'));
  assert.ok(m.calls.find(c=>c.name==='addLabels').args.labels.includes('needs:format'));
});

test('legacy gate requires no API status, body validation, or controller wait', async () => {
  let message;
  await gate({core:{info(value){message=value;}}});
  assert.ok(message.includes('does not wait'));
});


test('format feedback uses English for invalid and corrected PRs', async () => {
  for (const fresh of [{...pr, body: ''}, pr]) {
    const m = mock({fresh, comments: [{id: 5, user: {login: 'github-actions[bot]'}, body: '<!-- memoh-contribution-format:v1 -->\nPrevious feedback'}]});
    await inspectPR(m, 1);
    const comment = m.calls.find(call => call.name === 'updateComment').args.body;
    assert.match(comment, fresh.body ? /Format check passed/ : /Please complete/);
    assert.doesNotMatch(comment, /\p{Script=Han}/u);
    for (const call of m.calls.filter(call => call.name === 'createCommitStatus')) {
      assert.doesNotMatch(call.args.description, /\p{Script=Han}/u);
    }
  }
});
