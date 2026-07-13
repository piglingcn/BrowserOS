import { execSync } from 'node:child_process'

const allowedPattern =
  /^(feat|fix|bugfix|hotfix|release|docs|refactor|test|chore|experiment)\/[a-z0-9-]+$/

function getCurrentBranch(): string {
  return execSync('git rev-parse --abbrev-ref HEAD', {
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  }).trim()
}

const branch = getCurrentBranch()

if (branch === 'main' || branch === 'master') {
  process.exit(0)
}

if (!allowedPattern.test(branch)) {
  console.warn(
    `Warning: Branch name "${branch}" does not match the recommended format.`,
  )
  console.warn('Use: <type>/<short-description>')
  console.warn(
    'Types: feat, fix, bugfix, hotfix, release, docs, refactor, test, chore, experiment',
  )
  console.warn('Example: feat/add-auth, fix/login-crash')
  console.warn('')
  console.warn('To rename your branch:')
  console.warn('  git branch -m <new-name>')
  console.warn('  git push -u origin <new-name>')
}
