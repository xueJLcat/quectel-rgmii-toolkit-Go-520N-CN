// 面板进场动效(设计方案 §5.1):stagger 40ms/项,fade + y 12px→0,220ms ease-out;
// 仅动画 transform/opacity,不阻塞交互。firewall 各 Tab 的面板组共用。
import { motion } from "motion/react";
import type { Variants } from "motion/react";
import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

const PARENT: Variants = {
  hidden: {},
  show: { transition: { staggerChildren: 0.04 } },
};

const CHILD: Variants = {
  hidden: { opacity: 0, y: 12 },
  show: { opacity: 1, y: 0, transition: { duration: 0.22, ease: [0.22, 0.7, 0.28, 1] } },
};

export function StaggerGroup({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <motion.div
      variants={PARENT}
      initial="hidden"
      animate="show"
      className={cn("flex flex-col gap-4", className)}
    >
      {children}
    </motion.div>
  );
}

export function StaggerItem({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <motion.div variants={CHILD} className={className}>
      {children}
    </motion.div>
  );
}
