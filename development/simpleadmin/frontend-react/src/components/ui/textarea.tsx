import * as React from "react";

import { cn } from "@/lib/utils";

type TextareaProps = React.ComponentProps<"textarea"> & {
  invalid?: boolean;
};

function Textarea({ className, invalid, ...props }: TextareaProps) {
  return (
    <textarea
      data-slot="textarea"
      aria-invalid={invalid || undefined}
      className={cn(
        "flex field-sizing-content min-h-16 w-full rounded-sm border border-line bg-surface px-3 py-2 text-sm text-ink shadow-sm transition-[color,box-shadow] outline-none placeholder:text-muted disabled:cursor-not-allowed disabled:opacity-50",
        "focus-visible:border-accent focus-visible:ring-[3px] focus-visible:ring-accent/40",
        invalid && "border-danger focus-visible:border-danger focus-visible:ring-danger/40",
        className,
      )}
      {...props}
    />
  );
}

export { Textarea, type TextareaProps };
