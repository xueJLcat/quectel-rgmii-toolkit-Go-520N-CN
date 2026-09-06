import * as React from "react";

import { cn } from "@/lib/utils";

type InputProps = React.ComponentProps<"input"> & {
  invalid?: boolean;
};

function Input({ className, invalid, type, ...props }: InputProps) {
  return (
    <input
      type={type}
      data-slot="input"
      aria-invalid={invalid || undefined}
      className={cn(
        "flex h-9 w-full min-w-0 rounded-sm border border-line bg-surface px-3 py-1 text-sm text-ink shadow-sm transition-[color,box-shadow] outline-none file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-ink placeholder:text-muted selection:bg-accent selection:text-white disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50",
        "focus-visible:border-accent focus-visible:ring-[3px] focus-visible:ring-accent/40",
        invalid && "border-danger focus-visible:border-danger focus-visible:ring-danger/40",
        className,
      )}
      {...props}
    />
  );
}

export { Input, type InputProps };
