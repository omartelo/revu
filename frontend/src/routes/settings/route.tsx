import {
  Outlet,
  createFileRoute,
  useBlocker,
  useNavigate,
  useParams,
  useRouter,
} from "@tanstack/react-router"
import { ArrowLeft, Loader2, RotateCcw } from "lucide-react"

import { RouteErrorFallback } from "@/components/route-error-fallback"
import { SettingsFormContext } from "@/components/settings/settings-form-context"
import {
  SettingsSidebar,
  type SettingsSection,
} from "@/components/settings/settings-sidebar"
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
import { Button } from "@/components/ui/button"
import { Form } from "@/components/ui/form"
import { useActiveProfile } from "@/hooks/use-active-profile"
import { useSettingsForm } from "@/hooks/use-settings-form"

export const Route = createFileRoute("/settings")({
  component: SettingsLayout,
  errorComponent: RouteErrorFallback,
})

function SettingsLayout() {
  const router = useRouter()
  const navigate = useNavigate()
  const params = useParams({ strict: false })
  const section: SettingsSection = params.section ?? "sync"

  const { form, loading, saving, submit, discard, restoreDefaults } =
    useSettingsForm()
  const { profile: activeProfile } = useActiveProfile()

  const shouldBlock = section !== "accounts" && form.formState.isDirty

  const { proceed, reset, status } = useBlocker({
    shouldBlockFn: () => shouldBlock,
    withResolver: true,
  })

  const onSelectSection = (next: SettingsSection) => {
    if (next === section) return
    void navigate({
      to: "/settings/$section",
      params: { section: next },
      replace: true,
    })
  }

  const onBack = () => {
    router.history.back()
  }

  const onConfirmDiscard = async () => {
    await discard()
    proceed?.()
  }

  const canSave =
    section !== "accounts" &&
    form.formState.isDirty &&
    form.formState.isValid &&
    !saving

  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center bg-background text-muted-foreground">
        <Loader2 className="size-4 animate-spin" />
      </div>
    )
  }

  const showFooter = section !== "accounts"

  return (
    <SettingsFormContext.Provider value={form}>
      <div className="flex h-screen animate-in flex-col bg-background text-foreground duration-base ease-standard fade-in-0">
        <header className="flex items-center gap-2 border-b px-3 py-2">
          <Button
            size="sm"
            variant="ghost"
            onClick={onBack}
            aria-label="Voltar"
          >
            <ArrowLeft />
          </Button>
          <div className="font-heading text-base font-medium">
            Configurações
          </div>
        </header>

        <Form {...form}>
          <form
            onSubmit={(e) => {
              e.preventDefault()
              void submit()
            }}
            className="flex flex-1 flex-col overflow-hidden"
          >
            <div className="flex flex-1 overflow-hidden">
              <SettingsSidebar
                active={section}
                onSelect={onSelectSection}
                activeProfile={activeProfile}
              />
              <div className="flex-1 overflow-y-auto px-4 py-4">
                <Outlet />
              </div>
            </div>

            {showFooter ? (
              <footer className="flex items-center justify-between gap-2 border-t bg-muted/40 px-3 py-2">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={restoreDefaults}
                >
                  <RotateCcw />
                  Restaurar padrões
                </Button>
                <div className="flex items-center gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => void discard()}
                    disabled={!form.formState.isDirty || saving}
                  >
                    Descartar
                  </Button>
                  <Button type="submit" size="sm" disabled={!canSave}>
                    {saving ? <Loader2 className="animate-spin" /> : null}
                    Salvar
                  </Button>
                </div>
              </footer>
            ) : null}
          </form>
        </Form>

        <AlertDialog
          open={status === "blocked"}
          onOpenChange={(open) => {
            if (!open) reset?.()
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Descartar alterações?</AlertDialogTitle>
              <AlertDialogDescription>
                Você tem alterações não salvas nesta seção. Ao trocar, elas
                serão perdidas.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel onClick={() => reset?.()}>
                Cancelar
              </AlertDialogCancel>
              <AlertDialogAction onClick={() => void onConfirmDiscard()}>
                Descartar e continuar
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </SettingsFormContext.Provider>
  )
}
