// 蜂窝网络页(设计方案 §6.4):频段矩阵三列卡 + 锁频配置档 + 蜂窝设置表单。
// 数据流对齐旧版 www/js/pages/network.js:进入页面拉取 settings/bands(RQ 挂载即拉 =
// onPageReturn 语义),写操作 mutation+toast+延迟 3s 回读;频段勾选态 = 用户选择 ?? 服务端
// 已锁(_userTouchedBands 语义),恢复全部后复位跟随服务端;破坏性操作全部经 stores/confirm。
import { motion } from "motion/react";
import type { Variants } from "motion/react";
import { BookmarkIcon, RadioIcon, Settings2Icon } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";

import { ErrorRetry, StatusChip } from "@/components/common";
import { PageHeader } from "@/components/layout/page-header";
import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";

import { BandMatrix } from "./components/BandMatrix";
import { BandProfilesPanel } from "./components/BandProfilesPanel";
import { CellularSettingsForm } from "./components/CellularSettingsForm";
import {
  isActionFailure,
  useBandProfiles,
  useLockedBandsQuery,
  useLockBands,
  useNetworkSettingsQuery,
  useResetBands,
  useSaveSettings,
} from "./hooks";
import { BAND_DEFAULTS, buildBandGroups, joinBandValues, parseBandList } from "./lib/bandMap";
import type { BandMode, LockedBandsByMode } from "./lib/bandMap";
import { buildSaveSettingsPayload } from "./lib";
import type { BandProfile, CellularSettingsDraft } from "./lib";

// 面板入场 stagger 40ms(§5.1,与 App 路由转场同缓动)
const STAGGER_CONTAINER: Variants = {
  hidden: {},
  show: { transition: { staggerChildren: 0.04 } },
};
const STAGGER_ITEM: Variants = {
  hidden: { opacity: 0, y: 8 },
  show: { opacity: 1, y: 0, transition: { duration: 0.22, ease: [0.22, 0.7, 0.28, 1] } },
};

const EMPTY_DRAFT: CellularSettingsDraft = {
  newApn: null,
  newPdpType: null,
  prefNetworkMode: null,
  nrModeControlNew: null,
};

const LOCKED_FIELD_BY_MODE: Record<
  BandMode,
  "locked_lte_bands" | "locked_nsa_bands" | "locked_sa_bands"
> = {
  LTE: "locked_lte_bands",
  NSA: "locked_nsa_bands",
  SA: "locked_sa_bands",
};

export default function NetworkPage() {
  const { t } = useT("network");
  const confirm = useConfirmStore((state) => state.confirm);

  const settingsQuery = useNetworkSettingsQuery();
  const bandsQuery = useLockedBandsQuery();
  const lockBands = useLockBands();
  const resetBands = useResetBands();
  const saveSettings = useSaveSettings();
  const profilesApi = useBandProfiles();

  // null 语义:跟随服务端已锁频段(对齐旧版 _userTouchedBands=false);用户交互/应用配置档后置为显式数组
  const [bandSelection, setBandSelection] = useState<Partial<Record<BandMode, string[]>>>({});
  const [draft, setDraft] = useState<CellularSettingsDraft>(EMPTY_DRAFT);

  const lockedByMode = useMemo(() => {
    const locked: LockedBandsByMode = {};
    for (const mode of ["LTE", "NSA", "SA"] as const) {
      locked[mode] = (bandsQuery.bands?.[LOCKED_FIELD_BY_MODE[mode]] as string | undefined) ?? null;
    }
    return locked;
  }, [bandsQuery.bands]);

  const groups = useMemo(() => buildBandGroups(lockedByMode), [lockedByMode]);

  const checkedByMode = useMemo(() => {
    const result = {} as Record<BandMode, string[]>;
    for (const group of groups) {
      result[group.mode] =
        bandSelection[group.mode] ??
        group.bands.filter((band) => band.checked).map((band) => band.name);
    }
    return result;
  }, [groups, bandSelection]);

  const lockedNamesByMode = useMemo(() => {
    const result = {} as Record<BandMode, string[]>;
    for (const group of groups) result[group.mode] = parseBandList(lockedByMode[group.mode]);
    return result;
  }, [groups, lockedByMode]);

  const settings = settingsQuery.settings;
  const settingsPending = settingsQuery.data?.pending === true || settingsQuery.isPending;
  const settingsFailed = settingsQuery.isError || Boolean(settingsQuery.data?.error);
  const bandsPending = bandsQuery.data?.pending === true;
  const bandsFailed = bandsQuery.isError || Boolean(bandsQuery.data?.error);
  const bandsLoading = bandsQuery.bands === undefined;

  const snapshot = useMemo(
    () => ({
      apn: settings?.apn ?? "-",
      pdpType: settings?.pdpType ?? "-",
      prefNetwork: settings?.prefNetwork ?? "-",
      nrModeControlCurrent: settings?.nrModeControlNum ?? null,
    }),
    [settings],
  );

  const handleToggleBand = useCallback(
    (mode: BandMode, name: string) => {
      const current = checkedByMode[mode];
      const next = current.includes(name)
        ? current.filter((item) => item !== name)
        : [...current, name];
      setBandSelection((prev) => ({ ...prev, [mode]: next }));
    },
    [checkedByMode],
  );

  const handleToggleAll = useCallback(
    (mode: BandMode) => {
      const group = groups.find((item) => item.mode === mode);
      if (!group || group.bands.length === 0) return;
      const allNames = group.bands.map((band) => band.name);
      const allChecked = allNames.every((name) => checkedByMode[mode].includes(name));
      setBandSelection((prev) => ({ ...prev, [mode]: allChecked ? [] : allNames }));
    },
    [groups, checkedByMode],
  );

  const handleLockMode = useCallback(
    (mode: BandMode) => {
      const values = checkedByMode[mode];
      if (values.length === 0) {
        toast.error(t("noBandsSelectedSelectAtLeastOneBand"));
        return;
      }
      lockBands.mutate({ mode, values: joinBandValues(values) });
    },
    [checkedByMode, lockBands, t],
  );

  const handleResetAll = useCallback(async () => {
    const ok = await confirm({
      title: t("restoreAll"),
      message: t("thisWillRestoreBandLockingToAllBandsContinue"),
      danger: true,
    });
    if (!ok) return;
    resetBands.mutate(
      { lte: BAND_DEFAULTS.lte, nsa: BAND_DEFAULTS.nsa, sa: BAND_DEFAULTS.sa },
      {
        onSuccess: (result) => {
          // 恢复成功后勾选态回到"跟随服务端"(对齐旧版 _userTouchedBands=false)
          if (!isActionFailure(result)) setBandSelection({});
        },
      },
    );
  }, [confirm, resetBands, t]);

  const handleSave = useCallback(() => {
    const decision = buildSaveSettingsPayload(snapshot, draft);
    if (decision.kind === "no-change") {
      toast.info(t("noChangesWereMade"));
      return;
    }
    if (decision.kind === "not-ready") {
      toast.error(t("networkSettingsAreNotReadyYetPleaseTryAgainLater"));
      return;
    }
    saveSettings.mutate(decision.payload, {
      onSuccess: (result) => {
        if (!isActionFailure(result)) setDraft(EMPTY_DRAFT);
      },
    });
  }, [snapshot, draft, saveSettings, t]);

  const handleSaveProfile = useCallback(
    (name: string): boolean =>
      profilesApi.saveProfile(name, {
        bands: {
          LTE: checkedByMode.LTE,
          NSA: checkedByMode.NSA,
          SA: checkedByMode.SA,
        },
        prefNetworkMode: draft.prefNetworkMode,
        nrModeControlNew: draft.nrModeControlNew,
      }),
    [profilesApi, checkedByMode, draft],
  );

  const handleApplyProfile = useCallback(
    (profile: BandProfile) => {
      const bands = profile.data?.bands ?? {};
      const nextSelection: Partial<Record<BandMode, string[]>> = {};
      for (const mode of ["LTE", "NSA", "SA"] as const) {
        const names = bands[mode];
        nextSelection[mode] = Array.isArray(names) ? names.map(String) : [];
      }
      setBandSelection(nextSelection);
      setDraft((prev) => ({
        ...prev,
        prefNetworkMode: profile.data?.prefNetworkMode ?? null,
        nrModeControlNew: profile.data?.nrModeControlNew ?? null,
      }));
      toast.info(t("profileAppliedItOnlyFillsTheFormAndIsNotSubmittedAutomatically"));
    },
    [t],
  );

  const lockingMode = lockBands.isPending ? (lockBands.variables.mode as BandMode) : null;

  return (
    <>
      <PageHeader domain="network" title={t("nav:cellularNetwork")} />
      <motion.div
        variants={STAGGER_CONTAINER}
        initial="hidden"
        animate="show"
        className="flex flex-col gap-4"
      >
        <motion.div variants={STAGGER_ITEM}>
          <Panel
            domain="network"
            icon={RadioIcon}
            title={t("bandLock")}
            tools={
              bandsPending ? (
                <StatusChip tone="muted" pulse>
                  {t("queryingLockedBands")}
                </StatusChip>
              ) : undefined
            }
            footer={
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                <span className="truncate">
                  {t("activeBands")} {settings?.bands ?? "-"}
                </span>
                <Button
                  type="button"
                  variant="danger"
                  size="sm"
                  disabled={resetBands.isPending}
                  onClick={() => void handleResetAll()}
                >
                  {t("restoreAll")}
                </Button>
              </div>
            }
          >
            {bandsFailed ? (
              <ErrorRetry
                message={t("failedToLoadBandInformation")}
                onRetry={() => void bandsQuery.refetch()}
                retrying={bandsQuery.isFetching}
              />
            ) : (
              <BandMatrix
                groups={groups}
                checkedByMode={checkedByMode}
                lockedByMode={lockedNamesByMode}
                bandsLoading={bandsLoading}
                lockingMode={lockingMode}
                onToggleBand={handleToggleBand}
                onToggleAll={handleToggleAll}
                onLock={handleLockMode}
              />
            )}
          </Panel>
        </motion.div>

        <motion.div variants={STAGGER_ITEM}>
          <Panel domain="network" icon={BookmarkIcon} title={t("bandLockProfiles")}>
            <BandProfilesPanel
              profiles={profilesApi.profiles}
              onSaveProfile={handleSaveProfile}
              onApplyProfile={handleApplyProfile}
              onDeleteProfile={(profile) => profilesApi.deleteProfile(profile.name)}
            />
          </Panel>
        </motion.div>

        <motion.div variants={STAGGER_ITEM}>
          <Panel
            domain="network"
            icon={Settings2Icon}
            title={t("cellularSettings")}
            tools={
              settingsPending ? (
                <StatusChip tone="muted" pulse>
                  {t("common:moduleWarmingUp")}
                </StatusChip>
              ) : undefined
            }
          >
            {settingsFailed ? (
              <ErrorRetry
                message={t("common:failedToReadSettings")}
                onRetry={() => void settingsQuery.refetch()}
                retrying={settingsQuery.isFetching}
              />
            ) : (
              <CellularSettingsForm
                snapshot={snapshot}
                draft={draft}
                onDraftChange={(patch) => setDraft((prev) => ({ ...prev, ...patch }))}
                onSave={handleSave}
                saving={saveSettings.isPending}
                disabled={settingsFailed}
              />
            )}
          </Panel>
        </motion.div>
      </motion.div>
    </>
  );
}
