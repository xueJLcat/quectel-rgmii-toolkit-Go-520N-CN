import { motion } from "motion/react";
import { RadioTower } from "lucide-react";

/** fetchModuleModel 失败时的静默回退型号(与 login.html <title> 口径一致)。 */
export const DEFAULT_MODULE_MODEL = "RG520N-CN";

const MESH_BACKGROUND = [
  "radial-gradient(55rem 38rem at 12% 8%, color-mix(in oklab, var(--sa-accent) 68%, transparent), transparent 62%)",
  "radial-gradient(42rem 32rem at 88% 14%, color-mix(in oklab, var(--sa-accent-2) 55%, transparent), transparent 60%)",
  "radial-gradient(48rem 40rem at 72% 92%, color-mix(in oklab, var(--sa-accent-3) 48%, transparent), transparent 62%)",
  "linear-gradient(160deg, color-mix(in oklab, var(--sa-accent) 32%, black), color-mix(in oklab, var(--sa-accent-2) 24%, black))",
].join(", ");

const BLOB_STYLE_A = {
  background:
    "radial-gradient(circle, color-mix(in oklab, var(--sa-accent) 75%, transparent), transparent 70%)",
};

const BLOB_STYLE_B = {
  background:
    "radial-gradient(circle, color-mix(in oklab, var(--sa-accent-2) 65%, transparent), transparent 70%)",
};

/**
 * 桌面端(≥768px)左侧品牌英雄区:深蓝→accent mesh 渐变 + motion 缓慢浮动光斑
 * + 产品型号/SimpleAdmin 字样 + 装饰性信号波纹 SVG;暗色主题叠加压暗层。
 */
export function BrandPanel({ model }: { model: string }) {
  return (
    <aside
      className="relative hidden flex-col justify-between overflow-hidden p-10 text-white md:flex md:w-[46%] lg:w-[42%]"
      style={{ background: MESH_BACKGROUND }}
    >
      <div aria-hidden className="absolute inset-0 bg-black/5 dark:bg-black/35" />
      <motion.div
        aria-hidden
        className="pointer-events-none absolute -left-24 top-16 size-96 rounded-full blur-3xl"
        style={BLOB_STYLE_A}
        animate={{ x: [0, 48, -24, 0], y: [0, -36, 24, 0], scale: [1, 1.12, 0.94, 1] }}
        transition={{ duration: 22, repeat: Infinity, repeatType: "mirror", ease: "easeInOut" }}
      />
      <motion.div
        aria-hidden
        className="pointer-events-none absolute -right-20 bottom-10 size-80 rounded-full blur-3xl"
        style={BLOB_STYLE_B}
        animate={{ x: [0, -40, 20, 0], y: [0, 32, -28, 0], scale: [1, 0.9, 1.1, 1] }}
        transition={{
          duration: 26,
          delay: -8,
          repeat: Infinity,
          repeatType: "mirror",
          ease: "easeInOut",
        }}
      />

      <div className="relative flex items-center gap-3">
        <span className="flex size-10 items-center justify-center rounded-md bg-white/15 backdrop-blur">
          <RadioTower className="size-6" aria-hidden />
        </span>
        <span className="text-xl font-semibold tracking-wide">SimpleAdmin</span>
      </div>

      <div className="relative flex flex-col gap-2">
        <p className="text-sm font-medium tracking-[0.2em] text-white/65 uppercase">
          Quectel 5G CPE
        </p>
        <p className="font-mono text-4xl font-bold tracking-tight lg:text-5xl">{model}</p>
      </div>

      <svg
        aria-hidden
        viewBox="0 0 240 240"
        fill="none"
        className="relative size-52 self-end opacity-85 lg:size-64"
      >
        {[0, 1, 2, 3].map((ring) => (
          <motion.circle
            key={ring}
            cx="52"
            cy="192"
            r={34 + ring * 38}
            stroke="color-mix(in oklab, white 55%, transparent)"
            strokeWidth="2"
            animate={{ opacity: [0, 0.9, 0] }}
            transition={{
              duration: 3.2,
              repeat: Infinity,
              delay: ring * 0.6,
              ease: "easeOut",
            }}
          />
        ))}
        <circle cx="52" cy="192" r="8" fill="white" fillOpacity="0.9" />
      </svg>
    </aside>
  );
}

/** 移动端(<768px)顶部品牌条:桌面品牌区隐藏时保留品牌与型号信息。 */
export function MobileBrandBar({ model }: { model: string }) {
  return (
    <div className="flex items-center gap-3 border-b border-line bg-surface px-4 py-3 md:hidden">
      <span className="flex size-8 shrink-0 items-center justify-center rounded-sm bg-linear-to-br from-accent to-accent-2 text-white">
        <RadioTower className="size-4" aria-hidden />
      </span>
      <div className="flex min-w-0 flex-col leading-tight">
        <span className="text-sm font-semibold text-ink">SimpleAdmin</span>
        <span className="truncate font-mono text-xs text-muted">{model}</span>
      </div>
    </div>
  );
}
