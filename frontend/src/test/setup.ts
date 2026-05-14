import "@testing-library/jest-dom/vitest"
import "vitest-axe/extend-expect"
import { cleanup } from "@testing-library/react"
import { afterEach, expect } from "vitest"
import * as axeMatchers from "vitest-axe/matchers"

expect.extend(axeMatchers)

afterEach(() => {
  cleanup()
})
