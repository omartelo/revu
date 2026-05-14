import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import type { MergeMethod } from "@/lib/types"

interface PRMergeDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  prNumber: number
  prTitle: string
  method: MergeMethod | null
  onConfirm: () => void
  busy: boolean
  withApprove?: boolean
}

export function PRMergeDialog({
  open,
  onOpenChange,
  prNumber,
  prTitle,
  method,
  onConfirm,
  busy,
  withApprove = false,
}: PRMergeDialogProps) {
  const mergeLabel = method === "squash" ? "Squash & merge" : "Merge commit"
  const title = withApprove
    ? `Approve & ${mergeLabel}?`
    : `Confirmar ${mergeLabel}?`
  const busyLabel = withApprove ? "Aprovando + mergeando…" : "Executando…"
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="space-y-1">
              <div className="font-mono text-xs text-muted-foreground">
                #{prNumber}
              </div>
              <div className="text-sm">{prTitle}</div>
              <div className="pt-2 text-xs text-muted-foreground">
                Método: <span className="font-medium">{mergeLabel}</span>
                {withApprove && (
                  <>
                    {" "}
                    · vai aprovar o PR antes do merge (irreversível no GitHub)
                  </>
                )}
              </div>
            </div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy}>Cancelar</AlertDialogCancel>
          <AlertDialogAction
            onClick={(e) => {
              e.preventDefault()
              onConfirm()
            }}
            disabled={busy}
          >
            {busy ? busyLabel : "Confirmar"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
