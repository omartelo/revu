import { render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { axe } from "vitest-axe"

import type { PRRecord } from "@/lib/types"
import { createQueryWrapper } from "@/test/query-wrapper"

import { PRListTabs } from "./pr-list-tabs"

const getTrayAcknowledgedAt = vi.fn<() => Promise<string>>()

vi.mock("@/bridge", () => ({
  getTrayAcknowledgedAt: () => getTrayAcknowledgedAt(),
}))

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date("2026-04-30T12:00:00.000Z"))
  getTrayAcknowledgedAt.mockReset()
  getTrayAcknowledgedAt.mockResolvedValue("")
})

afterEach(() => {
  vi.useRealTimers()
})

function pr(id: string, overrides: Partial<PRRecord> = {}): PRRecord {
  return {
    id,
    number: 1,
    repo: "owner/repo",
    title: id,
    author: "alice",
    url: `https://github.com/owner/repo/pull/${id}`,
    state: "OPEN",
    isDraft: false,
    additions: 1,
    deletions: 1,
    reviewPending: true,
    reviewState: "PENDING",
    branch: "main",
    avatarUrl: "",
    firstSeenAt: "2026-04-30T11:55:00Z",
    lastSeenAt: "2026-04-30T11:55:00Z",
    ...overrides,
  }
}

function renderTabs(overrides: Partial<Parameters<typeof PRListTabs>[0]> = {}) {
  const { wrapper: Wrapper } = createQueryWrapper()
  const props = {
    pending: [],
    history: [],
    onOpenPR: vi.fn(),
    lastPollErr: null,
    onRetry: vi.fn(),
    initialLoading: false,
    ...overrides,
  } satisfies Parameters<typeof PRListTabs>[0]
  return render(
    <Wrapper>
      <PRListTabs {...props} />
    </Wrapper>
  )
}

describe("PRListTabs", () => {
  it("skeleton expõe role=status + aria-busy quando initialLoading e pending vazio", () => {
    renderTabs({ initialLoading: true })
    const status = screen.getByRole("status", { name: /Carregando PRs/i })
    expect(status).toHaveAttribute("aria-busy", "true")
    expect(status).toHaveAttribute("aria-live", "polite")
  })

  it("renderiza EmptyState pending quando lista vazia sem erro", () => {
    renderTabs()
    expect(screen.getByText("Tudo em dia ✦")).toBeInTheDocument()
  })

  it("renderiza EmptyState error-sync quando lastPollErr setado", () => {
    renderTabs({ lastPollErr: "rate limit" })
    expect(screen.getByText("Falha ao sincronizar")).toBeInTheDocument()
  })

  it("renderiza PR cards quando pending tem itens", () => {
    renderTabs({ pending: [pr("p-1"), pr("p-2")] })
    expect(screen.getAllByRole("button", { name: /p-1|p-2/ })).toHaveLength(2)
  })

  it("axe: skeleton sem violações", async () => {
    vi.useRealTimers()
    const { container } = renderTabs({ initialLoading: true })
    expect(await axe(container)).toHaveNoViolations()
  })

  it("axe: empty pending sem violações", async () => {
    vi.useRealTimers()
    const { container } = renderTabs()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("axe: lista populada sem violações", async () => {
    vi.useRealTimers()
    const { container } = renderTabs({ pending: [pr("p-1"), pr("p-2")] })
    expect(await axe(container)).toHaveNoViolations()
  })

  it("axe: erro de sync sem violações", async () => {
    vi.useRealTimers()
    const { container } = renderTabs({ lastPollErr: "rate limit" })
    expect(await axe(container)).toHaveNoViolations()
  })
})
