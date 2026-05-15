import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { axe } from "vitest-axe"

import { EmptyState } from "./empty-state"

describe("EmptyState", () => {
  it("variant pending: título + hint celebratório", () => {
    render(<EmptyState variant="pending" />)
    expect(screen.getByText("Tudo em dia ✦")).toBeInTheDocument()
    expect(
      screen.getByText("Nenhum PR aguardando seu review agora.")
    ).toBeInTheDocument()
  })

  it("variant history: título histórico vazio", () => {
    render(<EmptyState variant="history" />)
    expect(screen.getByText("Sem histórico ainda")).toBeInTheDocument()
  })

  it("a11y: usa role=status + aria-live=polite", () => {
    render(<EmptyState variant="pending" />)
    const region = screen.getByRole("status")
    expect(region).toHaveAttribute("aria-live", "polite")
  })

  it("ícones decorativos têm aria-hidden=true", () => {
    const { container } = render(<EmptyState variant="pending" />)
    const svgs = container.querySelectorAll("svg")
    expect(svgs.length).toBeGreaterThan(0)
    svgs.forEach((svg) => {
      expect(svg).toHaveAttribute("aria-hidden", "true")
    })
  })

  it("variant no-accounts: dispara onAddAccount ao clicar CTA", async () => {
    const user = userEvent.setup()
    const onAddAccount = vi.fn()
    render(<EmptyState variant="no-accounts" onAddAccount={onAddAccount} />)
    await user.click(screen.getByRole("button", { name: "Adicionar conta" }))
    expect(onAddAccount).toHaveBeenCalledOnce()
  })

  it("variant error-sync: renderiza message recebida", () => {
    render(
      <EmptyState
        variant="error-sync"
        message="rate limit atingido"
        onRetry={vi.fn()}
      />
    )
    expect(screen.getByText("Falha ao sincronizar")).toBeInTheDocument()
    expect(screen.getByText("rate limit atingido")).toBeInTheDocument()
  })

  it("variant error-sync: dispara onRetry ao clicar CTA", async () => {
    const user = userEvent.setup()
    const onRetry = vi.fn()
    render(
      <EmptyState variant="error-sync" message="falhou" onRetry={onRetry} />
    )
    await user.click(screen.getByRole("button", { name: "Tentar de novo" }))
    expect(onRetry).toHaveBeenCalledOnce()
  })

  it.each([["pending"] as const, ["history"] as const])(
    "axe: variant %s sem violações",
    async (variant) => {
      const { container } = render(<EmptyState variant={variant} />)
      expect(await axe(container)).toHaveNoViolations()
    }
  )

  it("axe: variant no-accounts sem violações", async () => {
    const { container } = render(
      <EmptyState variant="no-accounts" onAddAccount={vi.fn()} />
    )
    expect(await axe(container)).toHaveNoViolations()
  })

  it("axe: variant error-sync sem violações", async () => {
    const { container } = render(
      <EmptyState variant="error-sync" message="falhou" onRetry={vi.fn()} />
    )
    expect(await axe(container)).toHaveNoViolations()
  })
})
