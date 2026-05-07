import { useMutation, useQueryClient } from "@tanstack/react-query"

import { approveAndMergePR } from "@/bridge"
import type { ApproveAndMergeResult } from "@/bridge"
import { queryKeys } from "@/lib/query/keys"
import type { MergeMethod } from "@/lib/types"

interface ApproveAndMergePRArgs {
  prID: string
  method: MergeMethod
}

export function useApproveAndMergePR() {
  const qc = useQueryClient()
  return useMutation<ApproveAndMergeResult, Error, ApproveAndMergePRArgs>({
    mutationFn: ({ prID, method }) => approveAndMergePR(prID, method),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.prs.all })
    },
  })
}
