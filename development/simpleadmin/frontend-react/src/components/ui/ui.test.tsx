import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

beforeAll(() => {
  class ResizeObserverStub {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  }
  globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver;
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.setPointerCapture = () => {};
  Element.prototype.releasePointerCapture = () => {};
  Element.prototype.scrollIntoView = () => {};
});

describe("cn", () => {
  test("merges conflicting utilities with tailwind-merge", () => {
    expect(cn("p-2", "p-4")).toBe("p-4");
    expect(cn("text-sm", undefined, false, "font-bold")).toBe("text-sm font-bold");
    expect(cn("bg-accent", "bg-monitor")).toBe("bg-monitor");
    expect(cn("data-[state=checked]:bg-accent", "data-[state=checked]:bg-network")).toBe(
      "data-[state=checked]:bg-network",
    );
  });
});

describe("Button", () => {
  test("renders all four semantic variants with expected classes", () => {
    render(
      <>
        <Button variant="default">Primary</Button>
        <Button variant="secondary">Secondary</Button>
        <Button variant="ghost">Ghost</Button>
        <Button variant="danger">Danger</Button>
      </>,
    );
    expect(screen.getByRole("button", { name: "Primary" })).toHaveClass("from-accent");
    expect(screen.getByRole("button", { name: "Primary" })).toHaveClass("text-white");
    expect(screen.getByRole("button", { name: "Secondary" })).toHaveClass("bg-surface");
    expect(screen.getByRole("button", { name: "Secondary" })).toHaveClass("border-line-strong");
    expect(screen.getByRole("button", { name: "Ghost" })).toHaveClass("bg-transparent");
    expect(screen.getByRole("button", { name: "Ghost" })).toHaveClass("text-soft");
    expect(screen.getByRole("button", { name: "Danger" })).toHaveClass("bg-danger");
    expect(screen.getByRole("button", { name: "Danger" })).toHaveClass("text-white");
  });

  test("disabled button is inert and dimmed", () => {
    const onClick = vi.fn();
    render(
      <Button disabled onClick={onClick}>
        Save
      </Button>,
    );
    const button = screen.getByRole("button", { name: "Save" });
    expect(button).toBeDisabled();
    expect(button).toHaveClass("disabled:opacity-50");
    expect(button).toHaveClass("disabled:pointer-events-none");
    fireEvent.click(button);
    expect(onClick).not.toHaveBeenCalled();
  });
});

describe("Switch", () => {
  test("click toggles aria-checked", async () => {
    const user = userEvent.setup();
    render(<Switch aria-label="Enable watchdog" />);
    const toggle = screen.getByRole("switch", { name: "Enable watchdog" });
    expect(toggle).toHaveAttribute("aria-checked", "false");
    await user.click(toggle);
    expect(toggle).toHaveAttribute("aria-checked", "true");
    await user.click(toggle);
    expect(toggle).toHaveAttribute("aria-checked", "false");
  });

  test("checked color defaults to accent and can be overridden via className", () => {
    render(
      <>
        <Switch aria-label="Default" />
        <Switch aria-label="Domain" className="data-[state=checked]:bg-monitor" />
      </>,
    );
    expect(screen.getByRole("switch", { name: "Default" })).toHaveClass(
      "data-[state=checked]:bg-accent",
    );
    const domain = screen.getByRole("switch", { name: "Domain" });
    expect(domain).toHaveClass("data-[state=checked]:bg-monitor");
    expect(domain.className).not.toContain("bg-accent");
  });
});

describe("Dialog", () => {
  test("shows title when open and closes on Escape", async () => {
    const user = userEvent.setup();
    render(
      <Dialog>
        <DialogTrigger>Reboot</DialogTrigger>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirm reboot</DialogTitle>
            <DialogDescription>The device will restart immediately.</DialogDescription>
          </DialogHeader>
        </DialogContent>
      </Dialog>,
    );
    expect(screen.queryByRole("heading", { name: "Confirm reboot" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Reboot" }));
    expect(await screen.findByRole("heading", { name: "Confirm reboot" })).toBeInTheDocument();
    expect(screen.getByText("The device will restart immediately.")).toBeInTheDocument();
    await user.keyboard("{Escape}");
    await waitFor(() =>
      expect(screen.queryByRole("heading", { name: "Confirm reboot" })).not.toBeInTheDocument(),
    );
  });
});

describe("Tabs", () => {
  test("clicking a trigger switches the visible panel", async () => {
    const user = userEvent.setup();
    render(
      <Tabs defaultValue="overview">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="firewall">Firewall</TabsTrigger>
        </TabsList>
        <TabsContent value="overview">Overview panel</TabsContent>
        <TabsContent value="firewall">Firewall panel</TabsContent>
      </Tabs>,
    );
    expect(screen.getByText("Overview panel")).toBeInTheDocument();
    expect(screen.queryByText("Firewall panel")).not.toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Firewall" }));
    expect(await screen.findByText("Firewall panel")).toBeInTheDocument();
    expect(screen.queryByText("Overview panel")).not.toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Firewall" })).toHaveAttribute("data-state", "active");
  });
});

describe("Badge", () => {
  test("variant classes map to semantic tokens", () => {
    render(
      <>
        <Badge variant="default">Default</Badge>
        <Badge variant="success">Online</Badge>
        <Badge variant="warning">Degraded</Badge>
        <Badge variant="danger">Offline</Badge>
        <Badge variant="info">Info</Badge>
        <Badge variant="outline">Outline</Badge>
        <Badge variant="muted">Muted</Badge>
      </>,
    );
    expect(screen.getByText("Default")).toHaveClass("bg-accent-soft");
    expect(screen.getByText("Default")).toHaveClass("text-accent");
    expect(screen.getByText("Online")).toHaveClass("bg-success-soft");
    expect(screen.getByText("Degraded")).toHaveClass("bg-warning-soft");
    expect(screen.getByText("Offline")).toHaveClass("bg-danger-soft");
    expect(screen.getByText("Offline")).toHaveClass("text-danger");
    expect(screen.getByText("Info")).toHaveClass("bg-info-soft");
    expect(screen.getByText("Outline")).toHaveClass("border-line-strong");
    expect(screen.getByText("Muted")).toHaveClass("bg-surface-3");
    expect(screen.getByText("Online")).toHaveClass("rounded-sm");
  });
});

describe("Select", () => {
  // jsdom 无法驱动 Radix Select 的指针打开流程(官方已知限制),
  // 交互路径由 Playwright E2E 在真浏览器覆盖;此处用受控 open 验证内容渲染契约。
  test("closed trigger shows placeholder", () => {
    render(
      <Select>
        <SelectTrigger aria-label="Working mode">
          <SelectValue placeholder="Pick a mode" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="router">Router</SelectItem>
        </SelectContent>
      </Select>,
    );
    expect(screen.getByRole("combobox", { name: "Working mode" })).toHaveTextContent("Pick a mode");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  test("controlled open lists items", () => {
    render(
      <Select open>
        <SelectTrigger aria-label="Working mode">
          <SelectValue placeholder="Pick a mode" />
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            <SelectLabel>Modes</SelectLabel>
            <SelectItem value="router">Router</SelectItem>
            <SelectItem value="ap">Access Point</SelectItem>
            <SelectItem value="relay">Relay</SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>,
    );
    expect(screen.getByRole("listbox")).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Router" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Access Point" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Relay" })).toBeInTheDocument();
  });
});
