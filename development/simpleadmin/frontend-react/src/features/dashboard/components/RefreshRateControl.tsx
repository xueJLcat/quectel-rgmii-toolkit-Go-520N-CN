// 刷新频率控件(设计方案 §6.2:PageHeader actions,旧版标题栏 sa-refresh-input-group 语义):
// 数字输入 2–60s(默认 2,持久化 localStorage "refreshRate" 沿用旧版键),应用/Enter 生效并清空草稿,
// placeholder 显示当前频率(旧版 bindTitlebarControls 行为);旁挂更新时间(旧版 #dashboardLastUpdate)。
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useT } from "@/lib/i18n";
import { REFRESH_RATE_MAX, REFRESH_RATE_MIN } from "../lib";

export interface RefreshRateControlProps {
  rate: number;
  draft: string;
  onDraftChange: (value: string) => void;
  onApply: () => void;
  lastUpdate: string;
}

export function RefreshRateControl({
  rate,
  draft,
  onDraftChange,
  onApply,
  lastUpdate,
}: RefreshRateControlProps) {
  const { t } = useT("dashboard");
  const { t: tc } = useT();
  return (
    <div className="flex items-center gap-2">
      <span className="hidden text-sm text-muted xl:inline">{t("refreshRateMinimum2Seconds")}</span>
      <Input
        type="number"
        inputMode="numeric"
        min={REFRESH_RATE_MIN}
        max={REFRESH_RATE_MAX}
        aria-label={t("refreshRateMinimum2Seconds")}
        className="w-20"
        placeholder={`${rate}s`}
        value={draft}
        onChange={(event) => onDraftChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault();
            onApply();
          }
        }}
      />
      <Button variant="secondary" size="sm" onClick={onApply}>
        {tc("apply")}
      </Button>
      <span className="hidden text-sm text-muted lg:inline">
        {tc("lastUpdated")}{" "}
        <span data-slot="dashboard-last-update" className="font-mono text-soft">
          {lastUpdate || "-"}
        </span>
      </span>
    </div>
  );
}
