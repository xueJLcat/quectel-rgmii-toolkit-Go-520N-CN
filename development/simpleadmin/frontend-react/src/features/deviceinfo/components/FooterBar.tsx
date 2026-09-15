// 页脚版本条(设计方案 §6.16):版本号读 <meta name="sa-version">(lib.readSaVersion,回退空 → "-"),
// 代码库外链(target=_blank + noopener);13px muted flex-wrap(窄屏自动换行)。
import { useT } from "@/lib/i18n";
import { REPO_URL, readSaVersion } from "../lib";

export function FooterBar() {
  const { t } = useT("deviceinfo");
  const version = readSaVersion();
  return (
    <footer
      data-slot="deviceinfo-footer"
      className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted"
    >
      <span>
        {t("version")}{" "}
        <span data-slot="sa-version" className="font-mono text-soft">
          {version || "-"}
        </span>
      </span>
      <span aria-hidden="true">·</span>
      <a
        href={REPO_URL}
        target="_blank"
        rel="noopener noreferrer"
        className="text-accent hover:underline"
      >
        {t("codeRepository")}
      </a>
    </footer>
  );
}
