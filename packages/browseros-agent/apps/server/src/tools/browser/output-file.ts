import { randomUUID } from 'node:crypto'
import { join } from 'node:path'
import {
  getToolOutputDir,
  writeToolOutputBinaryFile,
  writeToolOutputFile,
} from '../../lib/browseros-dir'

function sanitizeSegment(value: string): string {
  const sanitized = value.replace(/[^a-z0-9_-]+/gi, '-').replace(/^-+|-+$/g, '')
  return sanitized || 'browser-tool-output'
}

function uniqueOutputPath(
  outputDir: string,
  toolName: string,
  extension: string,
): string {
  return join(
    outputDir,
    `${sanitizeSegment(toolName)}-${Date.now()}-${randomUUID()}.${
      sanitizeSegment(extension) || 'txt'
    }`,
  )
}

export async function writeTempToolOutputFile(args: {
  toolName: string
  extension: string
  content: string
}): Promise<string> {
  const outputDir = await getToolOutputDir()
  const filePath = uniqueOutputPath(outputDir, args.toolName, args.extension)
  await writeToolOutputFile(filePath, args.content)
  return filePath
}

export async function writeTempToolOutputBinaryFile(args: {
  toolName: string
  extension: string
  content: Uint8Array
}): Promise<string> {
  const outputDir = await getToolOutputDir()
  const filePath = uniqueOutputPath(outputDir, args.toolName, args.extension)
  await writeToolOutputBinaryFile(filePath, args.content)
  return filePath
}
