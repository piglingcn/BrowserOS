import { CockpitHero } from '@/components/cockpit/CockpitHero'
import { RecentActivity } from '@/components/cockpit/RecentActivity'
import { RunningGrid } from '@/components/cockpit/RunningGrid'
import { useCockpitData } from './cockpit.data'

/** Renders the Claw cockpit homepage. */
export function Cockpit() {
  const { agents } = useCockpitData()

  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-8 px-8 pt-8 pb-16">
      <CockpitHero />
      <RunningGrid agents={agents} />
      <RecentActivity />
    </div>
  )
}
