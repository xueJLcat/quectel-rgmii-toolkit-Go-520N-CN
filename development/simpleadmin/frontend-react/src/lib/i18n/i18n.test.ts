import i18n, { changeLanguage, LANGUAGE_STORAGE_KEY } from "@/lib/i18n";
import manifest from "@/lib/i18n/locales/manifest.json";

type NsBag = Record<string, string>;

function bundle(lng: "zh-CN" | "en", ns: string): NsBag {
  return (i18n.getResourceBundle(lng, ns) ?? {}) as NsBag;
}

function manifestCount(ns: string): number {
  return (manifest.counts as Record<string, number>)[ns];
}

function findKeyByZh(zh: string): { ns: string; id: string; en: string } {
  for (const ns of manifest.namespaces) {
    const zhBag = bundle("zh-CN", ns);
    for (const [id, value] of Object.entries(zhBag)) {
      if (value === zh) return { ns, id, en: bundle("en", ns)[id] };
    }
  }
  throw new Error(`zh-CN 资源中找不到条目: ${zh}`);
}

describe("资源加载", () => {
  it("每个命名空间在 zh-CN 与 en 都存在且条目数一致", () => {
    expect(manifest.namespaces.length).toBe(19);
    for (const ns of manifest.namespaces) {
      const zhBag = bundle("zh-CN", ns);
      const enBag = bundle("en", ns);
      expect(Object.keys(zhBag).length, `zh-CN 缺少 ns ${ns}`).toBeGreaterThan(0);
      expect(Object.keys(enBag).sort()).toEqual(Object.keys(zhBag).sort());
    }
  });

  it("init 使用 manifest 的 ns 数组,默认命名空间为 common", () => {
    expect(i18n.options.defaultNS).toBe(manifest.defaultNamespace);
    expect(i18n.options.ns).toEqual(expect.arrayContaining(manifest.namespaces));
    expect(manifest.defaultNamespace).toBe("common");
  });
});

describe("完整性(防丢条目)", () => {
  it("zh-CN 全部 ns 条目总数 >= 迁移脚本统计值(manifest.total)", () => {
    const total = manifest.namespaces.reduce(
      (sum, ns) => sum + Object.keys(bundle("zh-CN", ns)).length,
      0,
    );
    expect(total).toBeGreaterThanOrEqual(manifest.total);
    expect(manifest.total).toBe(manifest.dictionaryEntries + manifest.interpolationEntries);
  });

  it("每个 ns 的 zh-CN 与 en 条目数不少于 manifest.counts 且两语言一致", () => {
    // counts 为迁移基线(floor):开发期各页面代理可向自有 ns 增补词条,只防丢失不锁上限
    for (const ns of manifest.namespaces) {
      const zh = Object.keys(bundle("zh-CN", ns)).length;
      const en = Object.keys(bundle("en", ns)).length;
      expect(zh).toBeGreaterThanOrEqual(manifestCount(ns));
      expect(en).toBe(zh);
    }
  });

  it("动态句式模板的插值键全部写入 common", () => {
    expect(manifest.interpolationKeys.length).toBe(8);
    for (const id of manifest.interpolationKeys) {
      expect(bundle("zh-CN", "common")[id]).toBeTruthy();
      expect(bundle("en", "common")[id]).toBeTruthy();
    }
  });
});

describe("抽样翻译", () => {
  const samples = [
    "监控",
    "网络",
    "通信",
    "工具",
    "系统",
    "短信详情",
    "暗夜模式",
    "时间同步",
    "防火墙状态",
    "登录",
  ];

  it.each(samples)("%s 的英文翻译非空且不是中文原文", (zh) => {
    const { ns, id, en } = findKeyByZh(zh);
    expect(en).toBeTruthy();
    expect(en).not.toBe(zh);
    expect(i18n.t(`${ns}:${id}`, { lng: "en" })).toBe(en);
  });
});

describe("changeLanguage", () => {
  it("切换后 t() 输出、localStorage 与 <html lang> 同步更新", async () => {
    await changeLanguage("en");
    expect(i18n.language).toBe("en");
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe("en");
    expect(document.documentElement.lang).toBe("en");
    expect(i18n.t("nav:monitoring")).toBe("Monitoring");

    await changeLanguage("zh-CN");
    expect(i18n.language).toBe("zh-CN");
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe("zh-CN");
    expect(document.documentElement.lang).toBe("zh-CN");
    expect(i18n.t("nav:monitoring")).toBe("监控");
  });
});

describe("插值", () => {
  it("common:currentValue 等插值键正确展开 {{value}} 变量", () => {
    expect(i18n.t("common:currentValue", { value: "已启用", lng: "zh-CN" })).toBe("当前：已启用");
    expect(i18n.t("common:currentValue", { value: "Enabled", lng: "en" })).toBe("Current: Enabled");
    expect(i18n.t("common:getting", { thing: "IMEI", lng: "zh-CN" })).toBe("获取IMEI中...");
    expect(i18n.t("common:simActive", { index: 2, lng: "en" })).toBe("Active SIM 2");
    expect(i18n.t("common:dnsLineInvalid", { line: 3, lng: "en" })).toBe(
      "DNS server on line 3 has an invalid format",
    );
  });
});
