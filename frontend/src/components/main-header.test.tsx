import { render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { axe } from "vitest-axe"

import type { Profile } from "@/lib/types"
import { createQueryWrapper } from "@/test/query-wrapper"

import { MainHeader } from "./main-header"

const getActiveProfile = vi.fn<() => Promise<Profile>>()

vi.mock("@/bridge", () => ({
  getActiveProfile: () => getActiveProfile(),
}))

beforeEach(() => {
  getActiveProfile.mockReset()
  getActiveProfile.mockRejectedValue(new Error("no profile"))
})

afterEach(() => {
  vi.useRealTimers()
})

function renderHeader(
  overrides: Partial<Parameters<typeof MainHeader>[0]> = {}
) {
  const { wrapper: Wrapper } = createQueryWrapper()
  const props = {
    pendingCount: 3,
    historyCount: 7,
    lastPollAt: new Date("2026-04-30T11:55:00Z"),
    lastPollErr: null,
    loading: false,
    onRefresh: vi.fn(),
    onOpenSettings: vi.fn(),
    ...overrides,
  } satisfies Parameters<typeof MainHeader>[0]
  return render(
    <Wrapper>
      <MainHeader {...props} />
    </Wrapper>
  )
}

describe("MainHeader", () => {
  it("renderiza linha de status com role=status + aria-live=polite", () => {
    renderHeader()
    const status = screen.getByRole("status")
    expect(status).toHaveAttribute("aria-live", "polite")
    expect(status.textContent).toContain("3 pendentes")
    expect(status.textContent).toContain("7 no histórico")
  })

  it("singulariza pendente=1", () => {
    renderHeader({ pendingCount: 1 })
    expect(screen.getByRole("status").textContent).toMatch(/1 pendente /)
  })

  it("expõe role=alert quando lastPollErr setado", () => {
    renderHeader({ lastPollErr: "rate limit atingido" })
    const alert = screen.getByRole("alert")
    expect(alert.textContent).toContain("rate limit atingido")
  })

  it("botão Atualizar marca aria-busy quando loading", () => {
    renderHeader({ loading: true })
    const refresh = screen.getByRole("button", { name: /Atualizar/ })
    expect(refresh).toHaveAttribute("aria-busy", "true")
    expect(refresh).toBeDisabled()
  })

  it("botão Configurações usa aria-label", () => {
    renderHeader()
    expect(
      screen.getByRole("button", { name: "Configurações" })
    ).toBeInTheDocument()
  })

  it("axe: estado idle sem violações", async () => {
    const { container } = renderHeader()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("axe: estado loading sem violações", async () => {
    const { container } = renderHeader({ loading: true })
    expect(await axe(container)).toHaveNoViolations()
  })

  it("axe: com erro de poll sem violações", async () => {
    const { container } = renderHeader({ lastPollErr: "rate limit" })
    expect(await axe(container)).toHaveNoViolations()
  })
})
