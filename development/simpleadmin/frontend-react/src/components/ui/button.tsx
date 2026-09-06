import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-sm text-sm font-medium outline-none transition-all duration-[var(--sa-dur-fast)] ease-sa focus-visible:ring-[3px] focus-visible:ring-accent/40 disabled:pointer-events-none disabled:opacity-50 active:scale-[0.98] [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default:
          "bg-linear-to-br from-accent to-accent-2 text-white shadow-md hover:shadow-lg hover:brightness-110",
        secondary:
          "border border-line-strong bg-surface text-ink shadow-sm hover:bg-surface-2 hover:text-ink",
        ghost: "bg-transparent text-soft hover:bg-surface-2 hover:text-ink",
        danger: "bg-danger text-white shadow-md hover:shadow-lg hover:brightness-110",
        outline:
          "border border-line-strong bg-transparent text-ink shadow-sm hover:bg-surface-2 hover:text-ink",
      },
      size: {
        sm: "h-8 gap-1.5 px-3 text-xs has-[>svg]:px-2.5",
        default: "h-9 px-4 has-[>svg]:px-3",
        lg: "h-10 px-6 text-base has-[>svg]:px-4",
        icon: "size-9",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

type ButtonProps = React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean;
  };

function Button({ className, variant, size, asChild = false, ...props }: ButtonProps) {
  const Comp = asChild ? Slot : "button";
  return (
    <Comp
      data-slot="button"
      className={cn(buttonVariants({ variant, size }), className)}
      {...props}
    />
  );
}

export { Button, buttonVariants, type ButtonProps };
