import { readFileSync } from 'node:fs'

const messageFile = process.argv[2]
const firstLine = messageFile
  ? readFileSync(messageFile, 'utf8').split(/\r?\n/, 1)[0]
  : ''
const conventionalCommit =
  /^(feat|fix|docs|style|refactor|perf|test|chore|ci|build|revert)(\(.+\))?!?: .+/

if (!conventionalCommit.test(firstLine)) {
  process.stderr.write(`Commit message must follow Conventional Commits format:
  <type>(<optional scope>): <description>
  Types: feat, fix, docs, style, refactor, perf, test, chore, ci, build, revert

Examples:
  feat(auth): add OAuth2 support
  fix: resolve null pointer exception
`)
  process.exitCode = 1
}
