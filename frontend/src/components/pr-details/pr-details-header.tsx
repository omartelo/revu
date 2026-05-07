import { ArrowLeft, ExternalLink } from "lucide-react"

import { openPRInBrowser } from "@/bridge"
import { ReviewBadge } from "@/components/review-badge"
import { StatusBadge } from "@/components/status-badge"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { type PRFullDetails, type ReviewState, type PRState } from "@/lib/types"

import { MergeButton } from "./merge-button"

interface PRDetailsHeaderProps {
  details: PRFullDetails
  prState: PRState
  reviewState: ReviewState
  onBack: () => void
  canMerge: boolean
  mergeBlockReason: string | null
  merging: boolean
  onRequestMerge: (method: "squash" | "merge") => void
  onRequestApproveAndMerge: (method: "squash" | "merge") => void
}

export function PRDetailsHeader({
  details,
  prState,
  reviewState,
  onBack,
  canMerge,
  mergeBlockReason,
  merging,
  onRequestMerge,
  onRequestApproveAndMerge,
}: PRDetailsHeaderProps) {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <Button size="sm" variant="ghost" onClick={onBack} aria-label="Voltar">
          <ArrowLeft data-icon="inline-start" />
          Voltar
        </Button>
        <div className="flex items-center gap-1">
          <Button
            size="sm"
            variant="outline"
            onClick={() => void openPRInBrowser(details.url)}
          >
            <ExternalLink data-icon="inline-start" />
            Abrir no GitHub
          </Button>
          <MergeButton
            label="Approve & Squash"
            method="squash"
            primary={false}
            canMerge={canMerge}
            reason={mergeBlockReason}
            merging={merging}
            onClick={onRequestApproveAndMerge}
          />
          <MergeButton
            label="Squash"
            method="squash"
            primary
            canMerge={canMerge}
            reason={mergeBlockReason}
            merging={merging}
            onClick={onRequestMerge}
          />
          <MergeButton
            label="Merge"
            method="merge"
            primary={false}
            canMerge={canMerge}
            reason={mergeBlockReason}
            merging={merging}
            onClick={onRequestMerge}
          />
        </div>
      </div>

      <div className="flex flex-wrap items-start gap-2">
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <span className="font-mono text-sm text-muted-foreground">
            #{details.number}
          </span>
          <h1 className="truncate text-lg leading-tight font-semibold tracking-tight">
            {details.title}
          </h1>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <StatusBadge status={prState} />
          <ReviewBadge state={reviewState} />
        </div>
      </div>

      {details.labels.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {details.labels.map((l) => (
            <Badge
              key={l.name}
              variant="outline"
              className="border-transparent"
              style={{
                backgroundColor: `#${l.color}22`,
                color: `#${l.color}`,
              }}
            >
              {l.name}
            </Badge>
          ))}
        </div>
      )}
    </div>
  )
}
