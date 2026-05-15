// vitest-axe@0.1.0 só augmenta o namespace `Vi` (vitest 1/2). vitest 4 expõe
// `Assertion` via `declare module "@vitest/expect"` / `"vitest"`, então
// redeclaramos aqui pra que `expect(...).toHaveNoViolations()` tipechecque.
// Module augmentation requer `interface` e o param genérico `T` pra bater
// com a assinatura original — tslint/eslint reclamam dos vazios mas é
// padrão de declaration merging em TS.
/* eslint-disable @typescript-eslint/no-empty-object-type, @typescript-eslint/no-unused-vars */
import type { AxeMatchers } from "vitest-axe"

declare module "@vitest/expect" {
  interface Assertion<T = unknown> extends AxeMatchers {}
  interface AsymmetricMatchersContaining extends AxeMatchers {}
}

declare module "vitest" {
  interface Assertion<T = unknown> extends AxeMatchers {}
  interface AsymmetricMatchersContaining extends AxeMatchers {}
}
/* eslint-enable @typescript-eslint/no-empty-object-type, @typescript-eslint/no-unused-vars */
