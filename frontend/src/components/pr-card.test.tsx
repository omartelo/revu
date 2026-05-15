import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { axe } from "vitest-axe"

import type { PRRecord } from "@/lib/types"

import { PRCard } from "./pr-card"

function makePR(overrides: Partial<PRRecord> = {}): PRRecord {
  return {
    id: "pr-42",
    number: 42,
    repo: "owner/repo",
    title: "fix: bug crítico",
    author: "alice",
    url: "https://github.com/owner/repo/pull/42",
    state: "OPEN",
    isDraft: false,
    additions: 12,
    deletions: 3,
    reviewPending: true,
    reviewState: "PENDING",
    branch: "feat/fix-bug",
    avatarUrl: "https://avatars.githubusercontent.com/u/1?v=4",
    firstSeenAt: "2026-04-30T11:55:00Z",
    lastSeenAt: "2026-04-30T11:55:00Z",
    ...overrides,
  }
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date("2026-04-30T12:00:00.000Z"))
})

afterEach(() => {
  vi.useRealTimers()
})

describe("PRCard", () => {
  it("renderiza número, título, repo, autor e contadores de diff", () => {
    render(<PRCard pr={makePR()} onOpen={vi.fn()} />)

    expect(screen.getByText("#42")).toBeInTheDocument()
    expect(screen.getByText("fix: bug crítico")).toBeInTheDocument()
    expect(screen.getByText("owner/repo")).toBeInTheDocument()
    expect(screen.getByText("@alice")).toBeInTheDocument()
    expect(screen.getByText("+12")).toBeInTheDocument()
    expect(screen.getByText("−3")).toBeInTheDocument()
  })

  it("renderiza relTime baseado em lastSeenAt", () => {
    render(<PRCard pr={makePR()} onOpen={vi.fn()} />)
    expect(screen.getByText(/visto há 5min/)).toBeInTheDocument()
  })

  it("renderiza badges StatusBadge e ReviewBadge", () => {
    render(<PRCard pr={makePR()} onOpen={vi.fn()} />)
    expect(screen.getByText("OPEN")).toBeInTheDocument()
    expect(screen.getByText("revisar")).toBeInTheDocument()
  })

  it("usa role=button e tabIndex=0 pra acessibilidade", () => {
    render(<PRCard pr={makePR()} onOpen={vi.fn()} />)
    const card = screen.getByRole("button")
    expect(card).toHaveAttribute("tabIndex", "0")
  })

  it("dispara onOpen com pr.id ao clicar", async () => {
    vi.useRealTimers()
    const onOpen = vi.fn()
    const user = userEvent.setup()
    render(<PRCard pr={makePR({ id: "pr-99" })} onOpen={onOpen} />)

    await user.click(screen.getByRole("button"))
    expect(onOpen).toHaveBeenCalledExactlyOnceWith("pr-99")
  })

  it("dispara onOpen quando Enter é pressionado", async () => {
    vi.useRealTimers()
    const onOpen = vi.fn()
    const user = userEvent.setup()
    render(<PRCard pr={makePR({ id: "pr-99" })} onOpen={onOpen} />)

    const card = screen.getByRole("button")
    card.focus()
    await user.keyboard("{Enter}")
    expect(onOpen).toHaveBeenCalledExactlyOnceWith("pr-99")
  })

  it("dispara onOpen quando Space é pressionado", async () => {
    vi.useRealTimers()
    const onOpen = vi.fn()
    const user = userEvent.setup()
    render(<PRCard pr={makePR({ id: "pr-99" })} onOpen={onOpen} />)

    const card = screen.getByRole("button")
    card.focus()
    await user.keyboard(" ")
    expect(onOpen).toHaveBeenCalledExactlyOnceWith("pr-99")
  })

  it("não dispara onOpen pra outras teclas", async () => {
    vi.useRealTimers()
    const onOpen = vi.fn()
    const user = userEvent.setup()
    render(<PRCard pr={makePR()} onOpen={onOpen} />)

    const card = screen.getByRole("button")
    card.focus()
    await user.keyboard("a")
    expect(onOpen).not.toHaveBeenCalled()
  })

  it("renderiza badge DRAFT quando isDraft=true", () => {
    render(<PRCard pr={makePR({ isDraft: true })} onOpen={vi.fn()} />)
    expect(screen.getByText("DRAFT")).toBeInTheDocument()
  })

  it("renderiza badge APPROVED quando reviewState=APPROVED", () => {
    render(<PRCard pr={makePR({ reviewState: "APPROVED" })} onOpen={vi.fn()} />)
    expect(screen.getByText("aprovado")).toBeInTheDocument()
  })

  it("renderiza avatar do autor com alt=author quando avatarUrl está setado", () => {
    render(<PRCard pr={makePR()} onOpen={vi.fn()} />)
    const img = screen.getByAltText("alice")
    expect(img.tagName).toBe("IMG")
    expect(img.getAttribute("src")).toBe(
      "https://avatars.githubusercontent.com/u/1?v=4"
    )
  })

  it("renderiza fallback UserRound quando avatarUrl é vazio", () => {
    render(<PRCard pr={makePR({ avatarUrl: "" })} onOpen={vi.fn()} />)
    expect(screen.queryByAltText("alice")).toBeNull()
    expect(screen.getByLabelText("alice")).toBeInTheDocument()
  })

  it("renderiza branch name dentro de span font-mono", () => {
    render(<PRCard pr={makePR()} onOpen={vi.fn()} />)
    const branch = screen.getByText("feat/fix-bug")
    expect(branch).toBeInTheDocument()
    expect(branch.className).toMatch(/font-mono/)
  })

  it("não renderiza branch quando string vazia", () => {
    render(<PRCard pr={makePR({ branch: "" })} onOpen={vi.fn()} />)
    expect(screen.queryByText(/feat\//)).toBeNull()
  })

  it("renderiza dot novo quando isNew=true", () => {
    render(<PRCard pr={makePR()} onOpen={vi.fn()} isNew />)
    expect(screen.getByLabelText("novo")).toBeInTheDocument()
  })

  it("NÃO renderiza dot novo quando isNew=false (default)", () => {
    render(<PRCard pr={makePR()} onOpen={vi.fn()} />)
    expect(screen.queryByLabelText("novo")).toBeNull()
  })

  it("axe: estado base sem violações", async () => {
    vi.useRealTimers()
    const { container } = render(<PRCard pr={makePR()} onOpen={vi.fn()} />)
    expect(await axe(container)).toHaveNoViolations()
  })

  it("axe: com dot novo sem violações", async () => {
    vi.useRealTimers()
    const { container } = render(
      <PRCard pr={makePR()} onOpen={vi.fn()} isNew />
    )
    expect(await axe(container)).toHaveNoViolations()
  })

  it("axe: avatar fallback (UserRound) sem violações", async () => {
    vi.useRealTimers()
    const { container } = render(
      <PRCard pr={makePR({ avatarUrl: "" })} onOpen={vi.fn()} />
    )
    expect(await axe(container)).toHaveNoViolations()
  })
})
