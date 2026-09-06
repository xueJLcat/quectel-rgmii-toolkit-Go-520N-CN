// 账户安全卡:当前/新/确认密码 + 强度校验(非空/一致/无换行/8-128,前端拦截)→ setPassword。
// 成功 → confirm 提示后退出登录并跳转登录页(设计方案 §6.15 强制重登流程);
// 失败按后端 error 映射可读文案(对齐旧 changeLoginPassword 的 403/400 分支)。
import { KeyRoundIcon, LoaderCircleIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { Panel } from "@/components/layout/panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { logout } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { useConfirmStore } from "@/stores/confirm";

import { useChangePassword } from "../hooks";
import { passwordErrorInfo, validatePasswordForm } from "../lib";

export function PasswordCard() {
  const { t } = useT("settings");
  const confirm = useConfirmStore((state) => state.confirm);
  const changePassword = useChangePassword();
  const [form, setForm] = useState({ current: "", next: "", confirm: "" });

  function validationMessage(code: NonNullable<ReturnType<typeof validatePasswordForm>>): string {
    switch (code) {
      case "empty":
        return t("enterTheCurrentPasswordAndNewPassword");
      case "mismatch":
        return t("theNewPasswordsDoNotMatch");
      case "lineBreaks":
        return t("passwordNoLineBreaks");
      case "tooLong":
        return t("passwordTooLong");
      case "tooShort":
        return t("passwordTooShort");
    }
  }

  async function handleSubmit(): Promise<void> {
    const error = validatePasswordForm(form);
    if (error) {
      toast.warning(validationMessage(error));
      return;
    }
    try {
      await changePassword.mutateAsync(form);
      setForm({ current: "", next: "", confirm: "" });
      const relogin = await confirm({
        title: t("passwordSavedPleaseLogInAgainWithTheNewPassword"),
        confirmText: t("logOutAndSignInAgain"),
      });
      if (!relogin) return;
      try {
        await logout();
      } catch {
        // 注销请求失败不阻塞跳转,会话由后端自行过期(对齐 useLogout)
      }
      window.location.assign("/login.html");
    } catch (err) {
      const candidate = err as { status?: number; body?: string } | null;
      const info = passwordErrorInfo(candidate?.status, candidate?.body);
      toast.error(info.key ? t(info.key) : info.raw || t("passwordSaveFailed"));
    }
  }

  return (
    <Panel domain="system" icon={KeyRoundIcon} title={t("accountSecurity")}>
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <Label htmlFor="password-current">{t("currentPassword")}</Label>
          <Input
            id="password-current"
            type="password"
            autoComplete="current-password"
            placeholder={t("enterCurrentLoginPassword")}
            value={form.current}
            disabled={changePassword.isPending}
            onChange={(event) => setForm({ ...form, current: event.target.value })}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="password-new">{t("newLoginPassword")}</Label>
          <Input
            id="password-new"
            type="password"
            autoComplete="new-password"
            placeholder={t("enterNewLoginPassword")}
            value={form.next}
            disabled={changePassword.isPending}
            onChange={(event) => setForm({ ...form, next: event.target.value })}
          />
          <p className="text-sm text-muted">{t("passwordTooShort")}</p>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="password-confirm">{t("confirmNewLoginPassword")}</Label>
          <Input
            id="password-confirm"
            type="password"
            autoComplete="new-password"
            placeholder={t("enterNewLoginPasswordAgain")}
            value={form.confirm}
            disabled={changePassword.isPending}
            onChange={(event) => setForm({ ...form, confirm: event.target.value })}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                void handleSubmit();
              }
            }}
          />
        </div>
        <div>
          <Button onClick={() => void handleSubmit()} disabled={changePassword.isPending}>
            {changePassword.isPending && <LoaderCircleIcon className="animate-spin" />}
            {t("changePassword")}
          </Button>
        </div>
      </div>
    </Panel>
  );
}
