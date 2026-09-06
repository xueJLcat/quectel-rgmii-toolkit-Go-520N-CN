// 服务小区描述列表(设计方案 §6.3):双列 dl,label muted / 值 tabular-nums;
// 行构建与空值过滤在 lib.buildCellRows(制式/频点恒显,对齐旧版)。
import type { SignalCell } from "@/lib/api";
import { useT } from "@/lib/i18n";

import { buildCellRows } from "../lib";

export function CellInfo({ cell }: { cell?: SignalCell }) {
  const { t } = useT("signal");
  const rows = buildCellRows(cell);
  return (
    <dl className="grid grid-cols-1 gap-x-8 sm:grid-cols-2">
      {rows.map((row) => (
        <div
          key={row.key}
          className="flex items-baseline justify-between gap-3 border-b border-line py-2"
        >
          <dt className="text-sm text-muted">{t(row.labelKey)}</dt>
          <dd className="text-sm font-medium text-ink tabular-nums">{row.value}</dd>
        </div>
      ))}
    </dl>
  );
}
