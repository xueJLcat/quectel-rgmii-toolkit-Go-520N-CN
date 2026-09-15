// 锁频配置档面板(设计方案 §6.4 ③):档名输入 + 保存当前配置;档卡列表 =
// 名称 + 频段摘要 chips + 应用/删除(经 stores/confirm 危险确认)。
// 数据模型兼容旧版 localStorage 键 simpleadmin.bandProfiles(读写在 ../lib.ts)。
// 应用仅填充界面(频段勾选 + 首选网络 + NR5G 模式控制),不自动提交(对齐旧版 applyBandProfile)。
import { BookmarkIcon, PlayIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";

import { EmptyState } from "@/components/common";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";

import { BAND_GROUP_TITLES } from "../lib/bandMap";
import type { BandMode } from "../lib/bandMap";
import { formatBandSummary } from "../lib";
import type { BandProfile } from "../lib";

export interface BandProfilesPanelProps {
  profiles: BandProfile[];
  onSaveProfile: (name: string) => boolean;
  onApplyProfile: (profile: BandProfile) => void;
  onDeleteProfile: (profile: BandProfile) => void;
}

const SUMMARY_MODES: BandMode[] = ["LTE", "NSA", "SA"];

export function BandProfilesPanel({
  profiles,
  onSaveProfile,
  onApplyProfile,
  onDeleteProfile,
}: BandProfilesPanelProps) {
  const { t } = useT("network");
  const confirm = useConfirmStore((state) => state.confirm);
  const [name, setName] = useState("");

  async function handleDelete(profile: BandProfile): Promise<void> {
    const ok = await confirm({
      title: t("deleteProfile"),
      message: t("deleteThisProfile"),
      danger: true,
    });
    if (ok) onDeleteProfile(profile);
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
        <div className="flex flex-1 flex-col gap-2">
          <Label htmlFor="band-profile-name">{t("profileName")}</Label>
          <Input
            id="band-profile-name"
            type="text"
            autoComplete="off"
            placeholder={t("enterProfileName")}
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </div>
        <Button
          type="button"
          onClick={() => {
            if (onSaveProfile(name)) setName("");
          }}
        >
          <BookmarkIcon />
          {t("saveCurrentConfig")}
        </Button>
      </div>

      {profiles.length === 0 ? (
        <EmptyState title={t("noProfilesYet")} />
      ) : (
        <ul className="flex flex-col gap-2">
          {profiles.map((profile) => (
            <li
              key={profile.name}
              data-slot="band-profile-card"
              className="flex flex-col gap-2 rounded-md border border-line bg-surface p-3 sm:flex-row sm:items-center"
            >
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-semibold text-ink">{profile.name}</div>
                <div className="mt-1.5 flex flex-wrap gap-1">
                  {SUMMARY_MODES.flatMap((mode) => {
                    const names = profile.data?.bands?.[mode] ?? [];
                    if (names.length === 0) return [];
                    return [
                      <Badge key={mode} variant="outline">
                        {BAND_GROUP_TITLES[mode]}: {formatBandSummary(mode, names)}
                      </Badge>,
                    ];
                  })}
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Button
                  type="button"
                  size="sm"
                  variant="secondary"
                  onClick={() => onApplyProfile(profile)}
                >
                  <PlayIcon />
                  {t("common:apply")}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="danger"
                  aria-label={`${t("deleteProfile")} ${profile.name}`}
                  onClick={() => void handleDelete(profile)}
                >
                  <Trash2Icon />
                  {t("common:delete")}
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
