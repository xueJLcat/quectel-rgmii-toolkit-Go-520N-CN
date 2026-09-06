import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { motion, useAnimationControls } from "motion/react";
import { useTranslation } from "react-i18next";
import { Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";
import { login } from "@/lib/api/auth";
import type { LoginResult } from "@/lib/api/auth";

import { resolvePostLoginTarget } from "./sanitizeNext";

export interface LoginFormProps {
  /** 登录成功后的跳转函数,默认 location.assign;注入以便单测断言目标 URL。 */
  navigate?: (url: string) => void;
}

const defaultNavigate = (url: string): void => {
  window.location.assign(url);
};

const SHAKE_KEYFRAMES: { x: number[] } = { x: [0, -8, 8, -4, 4, 0] };
const SHAKE_OPTIONS = { duration: 0.4 };

/**
 * 登录表单卡:提交中 loading;失败卡片 shake + danger 错误文案(aria-live);
 * 429 按钮禁用并按 retry_after 每秒倒计时(login ns 无 {{seconds}} 插值词条,
 * 暂以 "tooManyAttemptsPleaseRetryIn" + N + "secondsBeforeRetry" 拼接,口径同旧版
 * www/login.html);成功后按 next → sessionStorage hash → "/" 恢复跳转。
 */
export function LoginForm({ navigate = defaultNavigate }: LoginFormProps) {
  const { t } = useTranslation("login");
  const shakeControls = useAnimationControls();

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [errorText, setErrorText] = useState("");
  const [countdown, setCountdown] = useState(0);

  useEffect(() => {
    if (countdown <= 0) return;
    const timer = window.setInterval(() => {
      setCountdown((current) => Math.max(0, current - 1));
    }, 1000);
    return () => window.clearInterval(timer);
  }, [countdown]);

  const locked = submitting || countdown > 0;
  const alertText =
    countdown > 0
      ? `${t("tooManyAttemptsPleaseRetryIn")}${countdown}${t("secondsBeforeRetry")}`
      : errorText;
  const buttonLabel =
    countdown > 0
      ? `${countdown}${t("secondsUntilRetry")}`
      : submitting
        ? t("signingIn")
        : t("signIn");

  function shake(): void {
    void shakeControls.start(SHAKE_KEYFRAMES, SHAKE_OPTIONS);
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (locked) return;
    setSubmitting(true);
    setErrorText("");
    try {
      const result: LoginResult = await login(username, password);
      if (result.ok) {
        navigate(resolvePostLoginTarget());
        return;
      }
      const retryAfter = Math.floor(result.retry_after ?? Number.NaN);
      if (Number.isFinite(retryAfter) && retryAfter > 0) {
        setCountdown(retryAfter);
      } else {
        setErrorText(
          /HTTP 5\d\d/.test(result.error ?? "")
            ? t("authenticationConfigurationError")
            : t("incorrectUsernameOrPassword"),
        );
        shake();
      }
    } catch {
      setErrorText(t("networkConnectionFailedPleaseCheckYourNetworkAndTryAgain"));
      shake();
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <motion.div animate={shakeControls} className="w-full max-w-sm">
      <Card className="bg-surface shadow-lg">
        <CardHeader>
          <CardTitle>{t("signIn")}</CardTitle>
          <CardDescription>{t("enterYourUsernameAndPassword")}</CardDescription>
        </CardHeader>
        <CardContent>
          <form autoComplete="on" className="flex flex-col gap-4" onSubmit={handleSubmit}>
            <div className="flex flex-col gap-2">
              <Label htmlFor="login-username">{t("username")}</Label>
              <Input
                id="login-username"
                name="username"
                type="text"
                autoComplete="username"
                autoFocus
                required
                invalid={Boolean(errorText)}
                value={username}
                onChange={(event) => setUsername(event.target.value)}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="login-password">{t("password")}</Label>
              <Input
                id="login-password"
                name="password"
                type="password"
                autoComplete="current-password"
                required
                invalid={Boolean(errorText)}
                value={password}
                onChange={(event) => setPassword(event.target.value)}
              />
            </div>
            <p
              aria-live="assertive"
              role="alert"
              className={cn("min-h-5 text-sm", alertText ? "text-danger" : "text-muted")}
            >
              {alertText}
            </p>
            <Button type="submit" size="lg" className="w-full" disabled={locked}>
              {submitting && <Loader2 aria-hidden className="animate-spin" />}
              {buttonLabel}
            </Button>
          </form>
        </CardContent>
      </Card>
    </motion.div>
  );
}
