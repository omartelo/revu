import type { MergeMethod, PRFullDetails, PRRecord } from "@/lib/types"

import { requireBridge } from "./client"
import { toPRRecord } from "./mappers"
import type { PRFullDetailsWire, PRRecordWire } from "./wire"

export type ApproveAndMergeStep = "approve" | "merge"

export interface ApproveAndMergeResult {
  failedStep?: ApproveAndMergeStep | ""
  approved: boolean
  errorMessage?: string
}

export interface PRsBridge {
  ListPendingPRs(): Promise<PRRecordWire[]>
  ListHistoryPRs(): Promise<PRRecordWire[]>
  GetPRDetails(prID: string): Promise<PRFullDetailsWire>
  GetPRDiff(prID: string): Promise<string>
  MergePR(prID: string, method: MergeMethod): Promise<void>
  ApproveAndMergePR(
    prID: string,
    method: MergeMethod
  ): Promise<ApproveAndMergeResult>
  RefreshNow(): Promise<void>
  OpenPRInBrowser(url: string): Promise<void>
  AcknowledgeTray(): Promise<void>
  GetTrayAcknowledgedAt(): Promise<string>
}

export async function listPendingPRs(): Promise<PRRecord[]> {
  return (await requireBridge("ListPendingPRs")()).map(toPRRecord)
}

export async function listHistoryPRs(): Promise<PRRecord[]> {
  return (await requireBridge("ListHistoryPRs")()).map(toPRRecord)
}

export const getPRDetails = (prID: string): Promise<PRFullDetails> =>
  requireBridge("GetPRDetails")(prID)

export const getPRDiff = (prID: string): Promise<string> =>
  requireBridge("GetPRDiff")(prID)

export const mergePR = (prID: string, method: MergeMethod): Promise<void> =>
  requireBridge("MergePR")(prID, method)

export const approveAndMergePR = (
  prID: string,
  method: MergeMethod
): Promise<ApproveAndMergeResult> =>
  requireBridge("ApproveAndMergePR")(prID, method)

export const refreshNow = (): Promise<void> => requireBridge("RefreshNow")()

export const openPRInBrowser = (url: string): Promise<void> =>
  requireBridge("OpenPRInBrowser")(url)

export const acknowledgeTray = (): Promise<void> =>
  requireBridge("AcknowledgeTray")()

// getTrayAcknowledgedAt resolves an ISO-8601 string or "" when never acked.
// Frontend converts to Date | null at the hook layer.
export const getTrayAcknowledgedAt = (): Promise<string> =>
  requireBridge("GetTrayAcknowledgedAt")()
