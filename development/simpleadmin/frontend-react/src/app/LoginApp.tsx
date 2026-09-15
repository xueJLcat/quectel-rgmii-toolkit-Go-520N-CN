import { useEffect, useState } from "react";

import { fetchModuleModel } from "@/lib/api/auth";
import { BrandPanel, DEFAULT_MODULE_MODEL, MobileBrandBar } from "@/features/login/BrandPanel";
import { LoginControls } from "@/features/login/LoginControls";
import { LoginForm } from "@/features/login/LoginForm";

/**
 * 登录页(MPA 独立入口,无 router):桌面分屏 = 左品牌英雄区 + 右表单卡;
 * 移动端只留顶部品牌条 + 表单卡。已登录访问由后端 303 重定向,前端无需探测。
 */
export default function LoginApp() {
  const [model, setModel] = useState(DEFAULT_MODULE_MODEL);

  useEffect(() => {
    let active = true;
    fetchModuleModel()
      .then((info) => {
        if (active && typeof info.model === "string" && info.model !== "") {
          setModel(info.model);
        }
      })
      .catch(() => {
        // 型号拉取失败静默回退硬编码 RG520N-CN
      });
    return () => {
      active = false;
    };
  }, []);

  return (
    <div className="flex min-h-svh flex-col bg-bg text-ink md:flex-row">
      <BrandPanel model={model} />
      <main className="relative flex flex-1 flex-col">
        <div className="absolute right-4 top-4 z-10">
          <LoginControls />
        </div>
        <MobileBrandBar model={model} />
        <div className="flex flex-1 items-center justify-center p-6">
          <LoginForm />
        </div>
      </main>
    </div>
  );
}
