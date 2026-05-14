import { render, screen } from "@testing-library/react"
import { type ReactNode } from "react"
import { useForm } from "react-hook-form"
import { describe, expect, it, vi } from "vitest"
import { axe } from "vitest-axe"

import { SettingsFormContext } from "@/components/settings/settings-form-context"
import { Form } from "@/components/ui/form"
import { ThemeProvider } from "@/lib/theme/theme-provider"
import { DEFAULT_CONFIG, type AppConfig } from "@/lib/types"

import { AppearanceSection } from "./appearance-section"

vi.mock("@/bridge", () => ({
  getTheme: vi.fn(() => Promise.resolve("light")),
  setTheme: vi.fn(() => Promise.resolve()),
}))

function FormHarness({ children }: { children: ReactNode }) {
  const form = useForm<AppConfig>({
    defaultValues: { ...DEFAULT_CONFIG } as AppConfig,
  })
  return (
    <SettingsFormContext.Provider value={form}>
      <Form {...form}>{children}</Form>
    </SettingsFormContext.Provider>
  )
}

function renderSection() {
  return render(
    <ThemeProvider>
      <FormHarness>
        <AppearanceSection />
      </FormHarness>
    </ThemeProvider>
  )
}

describe("AppearanceSection", () => {
  it("renderiza opções de tema com label/htmlFor batendo", () => {
    renderSection()
    expect(screen.getByLabelText(/Claro/)).toBeInTheDocument()
    expect(screen.getByLabelText(/Escuro/)).toBeInTheDocument()
    expect(screen.getByLabelText(/Auto/)).toBeInTheDocument()
  })

  it("renderiza inputs Largura e Altura com FormLabel associado", () => {
    renderSection()
    expect(screen.getByLabelText(/Largura/)).toBeInTheDocument()
    expect(screen.getByLabelText(/Altura/)).toBeInTheDocument()
  })

  it("axe: estado base sem violações", async () => {
    const { container } = renderSection()
    expect(await axe(container)).toHaveNoViolations()
  })
})
