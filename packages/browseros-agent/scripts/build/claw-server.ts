#!/usr/bin/env bun

import { runProdResourceBuild } from '@browseros/build-server-tools'

import { clawServerBuildProduct } from './claw-server/descriptor'

runProdResourceBuild(clawServerBuildProduct, process.argv.slice(2)).catch(
  (error) => {
    const message = error instanceof Error ? error.message : String(error)
    console.error(`\n✗ ${message}\n`)
    process.exit(1)
  },
)
