// 小区扫描卡(设计方案 §6.5 ②):扫描 = 长任务(按钮 loading + 进度提示文案,网关默认
// 超时 240s 覆盖"最长约 3 分钟");结果表每行 RSRP 内嵌 MetricBar + 选择列
// (NR5G 单选 / LTE 多选 ≤10,超限 toast 阻止),选中行高亮;"锁定所选" = NR+LTE
// 依次锁定(hooks useLockSelectedCells,对齐旧版顺序与失败中断),破坏性操作经 confirm。
// 扫描模式下拉:后端 scanModeCommand 仅支持 "Neighbour Scan"(QSCAN 系已从后端接口
// 移除,提交必然 400),故仅提供邻区扫描一项(旧版 celllock.js 亦硬编码该模式)。
import { LoaderCircleIcon, RadioTowerIcon, SearchIcon, XIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { MetricBar, signalQualityColor, useChartTheme } from "@/components/charts";
import { EmptyState } from "@/components/common";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { NeighbourCell } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";

import { isActionFailure, useCellScan, useLockSelectedCells } from "../hooks";
import { cellKey, rsrpPercent, toggleCellSelection } from "../lib";
import type { SelectedCell } from "../lib";

const SCAN_MODE_NEIGHBOUR = "Neighbour Scan";

interface ScanState {
  nrCells: NeighbourCell[];
  lteCells: NeighbourCell[];
  notice: string | null;
  done: boolean;
}

const EMPTY_SCAN: ScanState = { nrCells: [], lteCells: [], notice: null, done: false };

export function ScanCard() {
  const { t } = useT("celllock");
  const theme = useChartTheme();
  const confirm = useConfirmStore((state) => state.confirm);
  const scanMutation = useCellScan();
  const lockMutation = useLockSelectedCells();

  const [scanMode, setScanMode] = useState(SCAN_MODE_NEIGHBOUR);
  const [scan, setScan] = useState<ScanState>(EMPTY_SCAN);
  const [selected, setSelected] = useState<SelectedCell[]>([]);

  const scanning = scanMutation.isPending;
  const busy = scanning || lockMutation.isPending;
  const rows = [...scan.nrCells, ...scan.lteCells];

  function handleStartScan(): void {
    if (scanning) return;
    setSelected([]);
    setScan({ ...EMPTY_SCAN, notice: t("scanningMayTakeUpTo3MinutesPleaseStayOnThisPage") });
    scanMutation.mutate(scanMode, {
      onSuccess: (data) => {
        if (isActionFailure(data)) {
          // danger toast 已由 hook 统一发出;界面清空(对齐旧版失败路径)
          setScan(EMPTY_SCAN);
          return;
        }
        if (data.pending === true) {
          setScan({ ...EMPTY_SCAN, notice: t("moduleNotReadyYetPleaseRetryShortly") });
          return;
        }
        const nrCells = data.nr5g_cells_parsed ?? [];
        const lteCells = data.lte_cells_parsed ?? [];
        let notice: string | null = null;
        if (data.empty === true) {
          notice = t("scanFinishedButTheModuleReturnedNoCellsScanningMayBeLimitedByThisFirmware");
        } else if (nrCells.length + lteCells.length === 0) {
          notice = t("noCellsFound");
        }
        setScan({ nrCells, lteCells, notice, done: true });
      },
      onError: () => setScan(EMPTY_SCAN),
    });
  }

  function handleToggle(cell: NeighbourCell): void {
    const entry: SelectedCell = {
      pci: cell.pci ?? "",
      provider: cell.provider ?? "",
      type: cell.type ?? "",
      freq: cell.freq ?? "",
    };
    const result = toggleCellSelection(selected, entry);
    if (result.blocked === "lte-limit") {
      toast.error(t("youCanSelectUpTo10CellsToLock"));
      return;
    }
    setSelected(result.next);
  }

  async function handleLockSelected(): Promise<void> {
    if (selected.length === 0) {
      toast.error(t("pleaseSelectAtLeastOneCellToLock"));
      return;
    }
    const nr = selected.find((cell) => cell.type === "NR5G") ?? null;
    const ok = await confirm({
      title: t("lockCell"),
      message: nr
        ? t("nr5gCellLockProbesScsTheNetworkMayDropBrieflyUpTo25sContinue")
        : t("thisWillLockTheSelectedCellsContinue"),
      danger: true,
    });
    if (!ok) return;
    lockMutation.mutate(
      {
        nr,
        lte: selected.filter((cell) => cell.type !== "NR5G"),
        nrCells: scan.nrCells,
        lteCells: scan.lteCells,
      },
      {
        onSuccess: (outcome) => {
          if (outcome.ok) setSelected([]);
        },
      },
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted">
        {t(
          "cellScanWillScanAllLteAndNr5gSaCellsInYourAreaTheScanMayInterruptYourNetworkConnectionAndMayTakeAFewMinutes",
        )}
      </p>

      <div className="flex flex-wrap items-center gap-2">
        <Select value={scanMode} onValueChange={setScanMode} disabled={busy}>
          <SelectTrigger className="w-52" aria-label={t("selectScanMode")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={SCAN_MODE_NEIGHBOUR}>{t("neighbourCellScan")}</SelectItem>
          </SelectContent>
        </Select>
        <Button type="button" disabled={busy} onClick={handleStartScan}>
          {scanning ? <LoaderCircleIcon className="animate-spin" /> : <SearchIcon />}
          {scanning ? t("scanningPleaseWait") : t("startScan")}
        </Button>
        <Button
          type="button"
          variant="secondary"
          disabled={busy || selected.length === 0}
          onClick={() => void handleLockSelected()}
        >
          {t("lockSelectedCells")}
          {selected.length > 0 && (
            <span className="ml-1 rounded-full bg-surface-3 px-1.5 text-xs tabular-nums">
              {selected.length}
            </span>
          )}
        </Button>
        <Button
          type="button"
          variant="ghost"
          disabled={busy || !scan.done}
          onClick={() => {
            setSelected([]);
            setScan(EMPTY_SCAN);
          }}
        >
          <XIcon />
          {t("clear")}
        </Button>
      </div>

      {scanning ? (
        <div
          data-slot="scan-progress"
          className="flex items-center gap-3 rounded-md border border-line bg-surface-2 px-4 py-6 text-sm text-muted"
        >
          <LoaderCircleIcon className="size-5 shrink-0 animate-spin text-network" />
          {scan.notice ?? t("scanningMayTakeUpTo3MinutesPleaseStayOnThisPage")}
        </div>
      ) : rows.length > 0 ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-10">{t("select")}</TableHead>
              <TableHead>{t("colNetwork")}</TableHead>
              <TableHead>{t("colProvider")}</TableHead>
              <TableHead>{t("colBand")}</TableHead>
              <TableHead>{t("colFreq")}</TableHead>
              <TableHead>PCI</TableHead>
              <TableHead className="min-w-36">RSRP</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((cell) => {
              const entry: SelectedCell = {
                pci: cell.pci ?? "",
                provider: cell.provider ?? "",
                type: cell.type ?? "",
                freq: cell.freq ?? "",
              };
              const isSelected = selected.some((item) => cellKey(item) === cellKey(entry));
              const percent = rsrpPercent(cell.rsrp);
              return (
                <TableRow
                  key={cellKey(entry)}
                  data-slot="scan-row"
                  data-state={isSelected ? "selected" : undefined}
                  tabIndex={0}
                  className="cursor-pointer"
                  onClick={() => handleToggle(cell)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" || event.key === " ") {
                      event.preventDefault();
                      handleToggle(cell);
                    }
                  }}
                >
                  <TableCell>
                    <Checkbox
                      checked={isSelected}
                      tabIndex={-1}
                      aria-hidden="true"
                      className="pointer-events-none"
                    />
                  </TableCell>
                  <TableCell>{cell.type}</TableCell>
                  <TableCell>{cell.provider || "—"}</TableCell>
                  <TableCell>{cell.band}</TableCell>
                  <TableCell className="tabular-nums">{cell.freq}</TableCell>
                  <TableCell className="tabular-nums">{cell.pci}</TableCell>
                  <TableCell>
                    <div className="flex flex-col gap-1">
                      <span className="text-xs tabular-nums text-soft">{cell.rsrp ?? "—"} dBm</span>
                      <MetricBar
                        percent={percent}
                        color={signalQualityColor(percent, theme.mode)}
                      />
                    </div>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      ) : (
        <EmptyState
          icon={RadioTowerIcon}
          title={scan.notice ?? t("cellScan")}
          description={scan.notice ? undefined : t("startCellScan")}
        />
      )}
    </div>
  );
}
