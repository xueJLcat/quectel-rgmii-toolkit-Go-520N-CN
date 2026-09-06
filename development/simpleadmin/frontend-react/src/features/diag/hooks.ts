// diag 数据层:http_probe / dns_query 两个 mutation(经 /api/diag_data)。
// 运行时失败(200 + ok:false)是正常返回值,由调用方渲染为失败条目;
// 仅传输层失败(网关中断/400 参数错)会 reject,由调用方 catch 后同样落为失败条目。
import { useMutation } from "@tanstack/react-query";

import { diagData } from "@/lib/api";

export function useHttpProbe() {
  return useMutation({
    mutationFn: (target: string) => diagData("http_probe", { target }),
  });
}

export interface DnsQueryParams {
  domain: string;
  /** 留空走系统解析器(参数缺省,不发送空串) */
  server?: string;
}

export function useDnsQuery() {
  return useMutation({
    mutationFn: ({ domain, server }: DnsQueryParams) =>
      diagData("dns_query", server ? { domain, server } : { domain }),
  });
}
