import { execFileSync } from 'node:child_process';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { readLabels } from './sync-labels.mjs';
import { classify } from './contribution-policy.mjs';

const obsolete = ['bug/web','bug/server','bug/docker','bug/channel','feat/web','feat/server','feat/channel','channel','channel/telegram','channel/lark','channel/discord','ai-generate','acp','documentation','duplicate','question','roadmap'];
const renames = Object.fromEntries(['xs','s','m','l','xl'].map(size => [`size-${size}`, `size:${size.toUpperCase()}`]));
function api(path, method = 'GET', body) {
  const args = ['api',path,'--method',method];
  if (body) args.push('--input','-');
  const result = execFileSync('gh',args,{encoding:'utf8',maxBuffer:64*1024*1024,input:body ? JSON.stringify(body) : undefined});
  return result.trim() ? JSON.parse(result) : undefined;
}
function pages(path) {
  return JSON.parse(execFileSync('gh',['api',path,'--paginate','--slurp'],{encoding:'utf8',maxBuffer:64*1024*1024})).flat();
}

export async function migrate(repo, backup, apply) {
  if (!/^[\w.-]+\/[\w.-]+$/.test(repo ?? '')) throw new Error('Expected OWNER/REPO');
  const base=`repos/${repo}`;
  const desired=await readLabels();
  const current=pages(`${base}/labels?per_page=100`);
  const affected=current.filter(label=>obsolete.includes(label.name) || renames[label.name]);
  const assignments={};
  for (const label of affected) assignments[label.name]=pages(`${base}/issues?state=all&labels=${encodeURIComponent(label.name)}&per_page=100`).map(issue=>({number:issue.number,labels:issue.labels.map(item=>item.name)}));
  const prs=pages(`${base}/pulls?state=open&per_page=100`);
  const classifications=[];
  for (const pr of prs) {
    const detail=api(`${base}/pulls/${pr.number}`);
    const files=pages(`${base}/pulls/${pr.number}/files?per_page=100`);
    if(files.length!==detail.changed_files) throw new Error(`PR #${pr.number}: incomplete file list`);
    classifications.push({number:pr.number,sha:detail.head.sha,previous:pr.labels.map(label=>label.name),...classify(files)});
  }
  const snapshot={repo,createdAt:new Date().toISOString(),labels:current,assignments,classifications};
  if (!apply) { console.log(JSON.stringify(snapshot,null,2)); return; }
  if (!backup) throw new Error('--apply requires a backup directory');
  await mkdir(resolve(backup),{recursive:true});
  const backupFile=resolve(backup,`labels-${Date.now()}.json`);
  await writeFile(backupFile,JSON.stringify(snapshot,null,2)+'\n',{flag:'wx'});
  console.log(`Backup: ${backupFile}`);
  // Preserve label IDs/assignments by renaming first, then update colors/descriptions.
  for (const [oldName,newName] of Object.entries(renames)) {
    if (current.some(label=>label.name===oldName)) {
      if (current.some(label=>label.name===newName)) {
        for(const issue of assignments[oldName]) api(`${base}/issues/${issue.number}/labels`,'POST',{labels:[newName]});
        api(`${base}/labels/${encodeURIComponent(oldName)}`,'DELETE');
      } else api(`${base}/labels/${encodeURIComponent(oldName)}`,'PATCH',{new_name:newName});
    }
  }
  const existing=pages(`${base}/labels?per_page=100`);
  for(const label of desired) {
    const old=existing.find(item=>item.name.toLowerCase()===label.name.toLowerCase());
    if(old) api(`${base}/labels/${encodeURIComponent(old.name)}`,'PATCH',{new_name:label.name,color:label.color,description:label.description});
    else api(`${base}/labels`,'POST',label);
  }
  for(const name of obsolete.filter(name=>current.some(label=>label.name===name))) {
    const kind=name.startsWith('bug/')?'bug':name.startsWith('feat/')?'feat':undefined;
    if(kind) for(const issue of assignments[name]) if(!issue.labels.includes(kind)) api(`${base}/issues/${issue.number}/labels`,'POST',{labels:[kind]});
    api(`${base}/labels/${encodeURIComponent(name)}`,'DELETE');
  }
  for(const result of classifications) {
    const fresh=api(`${base}/pulls/${result.number}`);
    if(fresh.state!=='open' || fresh.head.sha!==result.sha) { console.log(`Skipped changed PR #${result.number}`); continue; }
    const wanted=[result.size,...result.changes];
    api(`${base}/issues/${result.number}/labels`,'POST',{labels:wanted});
    for(const label of fresh.labels.map(item=>item.name).filter(name=>(name.startsWith('size:')||name.startsWith('change:'))&&!wanted.includes(name))) api(`${base}/issues/${result.number}/labels/${encodeURIComponent(label)}`,'DELETE');
    console.log(`PR #${result.number}: +${result.additions}/-${result.deletions}, ${wanted.join(', ')}`);
  }
  const actual=pages(`${base}/labels?per_page=100`);
  for(const label of desired) {
    const found=actual.find(item=>item.name===label.name);
    if(!found || found.color.toLowerCase()!==label.color.toLowerCase() || found.description!==label.description) throw new Error(`Label verification failed: ${label.name}`);
  }
  console.log(`Verified ${desired.length} configured labels; ${actual.length} repository labels total.`);
}
if(process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href) {
  const [, , repo,backup,...flags]=process.argv;
  await migrate(repo,backup,flags.includes('--apply'));
}
