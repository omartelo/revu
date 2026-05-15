import { AlertCircle, RefreshCw, Settings } from "lucide-react"

import { Logo } from "@/components/logo"
import { MainHeaderProfileBadge } from "@/components/main-header-profile-badge"
import type { SettingsSection } from "@/components/settings/settings-sidebar"
import { Button } from "@/components/ui/button"
import { useRelativeTime } from "@/hooks/use-relative-time"
import { cn } from "@/lib/utils"

interface MainHeaderProps {
  pendingCount: number
  historyCount: number
  lastPollAt: Date | null
  lastPollErr: string | null
  loading: boolean
  onRefresh: () => void
  onOpenSettings: (section?: SettingsSection) => void
}

export function MainHeader({
  pendingCount,
  historyCount,
  lastPollAt,
  lastPollErr,
  loading,
  onRefresh,
  onOpenSettings,
}: MainHeaderProps) {
  const since = useRelativeTime(lastPollAt, {
    idleLabel: "ainda não atualizado",
    prefix: "atualizado ",
  })

  return (
    <header className="flex items-start justify-between gap-2">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-1.5">
            <Logo className="size-5 text-primary" decorative />
            <span className="font-heading text-base font-medium">revu</span>
          </div>
          <MainHeaderProfileBadge
            onOpenAccounts={() => onOpenSettings("accounts")}
          />
        </div>
        <div
          role="status"
          aria-live="polite"
          className="truncate text-xs text-muted-foreground"
        >
          {pendingCount} pendente{pendingCount === 1 ? "" : "s"} ·{" "}
          {historyCount} no histórico · {since}
        </div>
        {lastPollErr && (
          <div
            role="alert"
            className="mt-0.5 flex items-center gap-1 text-xs text-destructive"
          >
            <AlertCircle className="size-3" aria-hidden="true" />
            último poll falhou: {lastPollErr}
          </div>
        )}
      </div>
      <div className="flex items-center gap-1">
        <Button
          size="sm"
          variant="ghost"
          onClick={() => onOpenSettings()}
          aria-label="Configurações"
        >
          <Settings aria-hidden="true" />
        </Button>
        <Button
          size="sm"
          variant="outline"
          onClick={onRefresh}
          disabled={loading}
          aria-busy={loading}
        >
          <RefreshCw
            data-icon="inline-start"
            aria-hidden="true"
            className={cn(loading && "animate-spin")}
          />
          Atualizar
        </Button>
      </div>
    </header>
  )
}
