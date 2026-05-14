import { EmptyState } from "@/components/empty-state"
import { PRCard } from "@/components/pr-card"
import { PRCardSkeleton } from "@/components/pr-card-skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useTrayAcknowledgedAt } from "@/hooks/use-prs"
import type { PRRecord } from "@/lib/types"

const INITIAL_LOAD_SKELETON_COUNT = 4

interface PRListTabsProps {
  pending: PRRecord[]
  history: PRRecord[]
  onOpenPR: (prID: string) => void
  lastPollErr: string | null
  onRetry: () => void
  initialLoading: boolean
}

// isNewSince devolve true quando firstSeenAt é posterior ao último ack do
// tray. Antes do primeiro ack (acked=null), nada é "novo" — semântica de
// "clean slate" no boot inicial.
function isNewSince(firstSeenAt: string, acked: Date | null): boolean {
  if (!acked) return false
  return new Date(firstSeenAt).getTime() > acked.getTime()
}

export function PRListTabs({
  pending,
  history,
  onOpenPR,
  lastPollErr,
  onRetry,
  initialLoading,
}: PRListTabsProps) {
  const acked = useTrayAcknowledgedAt()
  return (
    <Tabs
      defaultValue="pending"
      className="flex flex-1 flex-col overflow-hidden"
    >
      <TabsList>
        <TabsTrigger value="pending">
          Pendentes
          {pending.length > 0 && (
            <span className="ml-1.5 rounded-full bg-primary/15 px-1.5 text-[10px] font-medium text-primary">
              {pending.length}
            </span>
          )}
        </TabsTrigger>
        <TabsTrigger value="history">Histórico</TabsTrigger>
      </TabsList>

      <TabsContent value="pending" className="flex-1 overflow-y-auto px-2 pt-2">
        {initialLoading && pending.length === 0 ? (
          <SkeletonList />
        ) : pending.length === 0 ? (
          lastPollErr ? (
            <EmptyState
              variant="error-sync"
              message={lastPollErr}
              onRetry={onRetry}
            />
          ) : (
            <EmptyState variant="pending" />
          )
        ) : (
          <div className="flex flex-col gap-2">
            {pending.map((pr) => (
              <PRCard
                key={pr.id}
                pr={pr}
                onOpen={onOpenPR}
                isNew={isNewSince(pr.firstSeenAt, acked)}
              />
            ))}
          </div>
        )}
      </TabsContent>

      <TabsContent value="history" className="flex-1 overflow-y-auto px-2 pt-2">
        {initialLoading && history.length === 0 ? (
          <SkeletonList />
        ) : history.length === 0 ? (
          <EmptyState variant="history" />
        ) : (
          <div className="flex flex-col gap-2">
            {history.map((pr) => (
              <PRCard
                key={pr.id}
                pr={pr}
                onOpen={onOpenPR}
                isNew={isNewSince(pr.firstSeenAt, acked)}
              />
            ))}
          </div>
        )}
      </TabsContent>
    </Tabs>
  )
}

function SkeletonList() {
  return (
    <div className="flex flex-col gap-2">
      {Array.from({ length: INITIAL_LOAD_SKELETON_COUNT }).map((_, i) => (
        <PRCardSkeleton key={i} />
      ))}
    </div>
  )
}
