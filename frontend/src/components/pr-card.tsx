import { ChevronRight, GitBranch, UserRound } from "lucide-react"

import { ReviewBadge } from "@/components/review-badge"
import { StatusBadge } from "@/components/status-badge"
import { Card, CardContent } from "@/components/ui/card"
import { useRelativeTime } from "@/hooks/use-relative-time"
import { type PRRecord, reviewStateOf, statusOf } from "@/lib/types"

interface PRCardProps {
  pr: PRRecord
  onOpen: (prID: string) => void
  // isNew sinaliza o estado "novo desde última visualização" (REV-54).
  // Computado uma vez no list container a partir de useTrayAcknowledgedAt
  // pra evitar 1 query react-query por card.
  isNew?: boolean
}

export function PRCard({ pr, onOpen, isNew = false }: PRCardProps) {
  const status = statusOf(pr)
  const review = reviewStateOf(pr)
  const seen = useRelativeTime(pr.lastSeenAt, { prefix: "visto " })

  function handleClick() {
    onOpen(pr.id)
  }

  function handleKey(e: React.KeyboardEvent<HTMLDivElement>) {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault()
      onOpen(pr.id)
    }
  }

  return (
    <Card
      size="sm"
      role="button"
      tabIndex={0}
      onClick={handleClick}
      onKeyDown={handleKey}
      className="cursor-pointer shadow-sm outline-none ring-0 transition-[box-shadow,transform] duration-base ease-standard hover:shadow-md hover:ring-1 hover:ring-primary/30 hover:-translate-y-px focus-visible:ring-2 focus-visible:ring-ring active:translate-y-0 active:shadow-sm"
    >
      <div className="flex items-start gap-3 px-3">
        <div className="relative shrink-0">
          {pr.avatarUrl ? (
            <img
              src={pr.avatarUrl}
              alt={pr.author}
              loading="lazy"
              referrerPolicy="no-referrer"
              className="size-8 rounded-full bg-muted object-cover"
            />
          ) : (
            <div
              aria-label={pr.author}
              className="flex size-8 items-center justify-center rounded-full bg-muted text-muted-foreground"
            >
              <UserRound className="size-4" aria-hidden="true" />
            </div>
          )}
          {isNew && (
            <span
              data-testid="pr-novo-dot"
              aria-label="novo"
              className="absolute -top-0.5 -right-0.5 block size-2.5 rounded-full bg-primary ring-2 ring-card"
            />
          )}
        </div>

        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <div className="flex items-baseline gap-2 truncate">
            <span className="font-mono text-xs text-muted-foreground">
              #{pr.number}
            </span>
            <span className="truncate text-sm leading-tight font-semibold">
              {pr.title}
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 truncate text-xs text-muted-foreground">
            <span className="truncate">{pr.repo}</span>
            <span aria-hidden="true">·</span>
            <span>@{pr.author}</span>
            {pr.branch !== "" && (
              <>
                <span aria-hidden="true">·</span>
                <span className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 font-mono text-[0.7rem]">
                  <GitBranch className="size-3" aria-hidden="true" />
                  {pr.branch}
                </span>
              </>
            )}
          </div>
        </div>

        <div className="flex shrink-0 flex-col items-end gap-1">
          <StatusBadge status={status} />
          <ReviewBadge state={review} />
        </div>
      </div>

      <CardContent className="flex items-center justify-between gap-2 text-xs leading-snug text-muted-foreground">
        <div className="flex items-center gap-2">
          <span className="font-mono font-medium text-emerald-600 dark:text-emerald-400">
            +{pr.additions}
          </span>
          <span className="font-mono font-medium text-rose-600 dark:text-rose-400">
            −{pr.deletions}
          </span>
        </div>
        <div className="flex items-center gap-1">
          <span>{seen}</span>
          <ChevronRight className="size-3" aria-hidden="true" />
        </div>
      </CardContent>
    </Card>
  )
}
