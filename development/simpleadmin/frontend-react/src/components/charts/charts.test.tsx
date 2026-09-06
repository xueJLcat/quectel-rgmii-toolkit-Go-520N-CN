import { render } from "@testing-library/react";
import {
  EChart,
  GaugeChart,
  HeatBar,
  MetricBar,
  applyChartTheme,
  barCompareOption,
  countdownGaugeOption,
  defaultChartTheme,
  gaugeOption,
  heatThresholdColor,
  signalQualityColor,
  sparklineOption,
  trendLineOption,
} from "./index";
import type { EChartsOption } from "./index";

type Series = Record<string, unknown>;

function series0(opt: EChartsOption): Series {
  const s = opt.series;
  return (Array.isArray(s) ? s[0] : s) as unknown as Series;
}

const light = defaultChartTheme("light");
const dark = defaultChartTheme("dark");

describe("gaugeOption", () => {
  it("半环 gauge:progress 800ms cubic-out、startAngle/endAngle、value、渐变描边、中心数字与标题", () => {
    const opt = gaugeOption({
      value: 42,
      max: 100,
      label: "%",
      title: "CPU",
      colorStops: ["#10b981", "#14b8a6"],
      theme: light,
    });
    const g = series0(opt);
    expect(g.type).toBe("gauge");
    expect(g.startAngle).toBe(210);
    expect(g.endAngle).toBe(-30);
    expect(g.animationDuration).toBe(800);
    expect(g.animationEasing).toBe("cubicOut");

    const data = g.data as { value: number; name: string }[];
    expect(data[0].value).toBe(42);
    expect(data[0].name).toBe("CPU");

    const detail = g.detail as { formatter: string; fontSize: number; fontWeight: number };
    expect(detail.formatter).toBe("{value}%");
    expect(detail.fontSize).toBe(24);
    expect(detail.fontWeight).toBe(700);

    const progress = g.progress as { itemStyle: { color: { colorStops: { color: string }[] } } };
    const stops = progress.itemStyle.color.colorStops;
    expect(stops[0].color).toBe("#10b981");
    expect(stops[stops.length - 1].color).toBe("#14b8a6");
  });

  it("单色 colorStops 退化为纯色描边;暗色主题文本色参数化", () => {
    const opt = gaugeOption({ value: 10, colorStops: ["#f43f5e"], theme: dark });
    const g = series0(opt);
    const progress = g.progress as { itemStyle: { color: unknown } };
    expect(progress.itemStyle.color).toBe("#f43f5e");
    const detail = g.detail as { color: string };
    expect(detail.color).toBe(dark.text);
  });

  it("非有限 value/max 容错", () => {
    expect(() => gaugeOption({ value: Number.NaN, max: 0, theme: light })).not.toThrow();
    const g = series0(gaugeOption({ value: Number.NaN, max: 0, theme: light }));
    expect((g.data as { value: number }[])[0].value).toBe(0);
    expect(g.max).toBe(100);
  });
});

describe("signalQualityColor 四档 × 明暗", () => {
  it("亮色:优秀 emerald / 良好 lime / 一般 amber / 差 rose", () => {
    expect(signalQualityColor("excellent", "light")).toBe("#10b981");
    expect(signalQualityColor("good", "light")).toBe("#84cc16");
    expect(signalQualityColor("fair", "light")).toBe("#f59e0b");
    expect(signalQualityColor("poor", "light")).toBe("#f43f5e");
  });

  it("暗色:四档取更亮的对称值", () => {
    expect(signalQualityColor("excellent", "dark")).toBe("#34d399");
    expect(signalQualityColor("good", "dark")).toBe("#a3e635");
    expect(signalQualityColor("fair", "dark")).toBe("#fbbf24");
    expect(signalQualityColor("poor", "dark")).toBe("#fb7185");
  });

  it("百分比入参按阈值映射到对应档", () => {
    expect(signalQualityColor(85)).toBe("#10b981"); // excellent
    expect(signalQualityColor(65)).toBe("#84cc16"); // good
    expect(signalQualityColor(55)).toBe("#f59e0b"); // fair
    expect(signalQualityColor(20)).toBe("#f43f5e"); // poor
    expect(signalQualityColor(Number.NaN)).toBe("#f43f5e"); // 非有限 → poor
  });
});

describe("trendLineOption 容错", () => {
  it("空数组:双 y 轴 + 三系列,不抛错", () => {
    expect(() => trendLineOption({ points: [], theme: light })).not.toThrow();
    const opt = trendLineOption({ points: [], theme: light });
    expect(opt.yAxis as unknown[]).toHaveLength(2);
    expect(opt.series as unknown[]).toHaveLength(3);
    const xAxis = opt.xAxis as { data: string[] };
    expect(xAxis.data).toEqual([]);
  });

  it("字段缺省:缺失值映射为 null,不影响其它系列", () => {
    const opt = trendLineOption({
      points: [{ t: 1_700_000_000 }, { sig: 55 }, { rxr: 120, txr: 30 }],
      theme: light,
    });
    const s = opt.series as unknown as Series[];
    expect(s[0].data as (number | null)[]).toEqual([null, 55, null]); // 信号 sig
    expect(s[1].data as (number | null)[]).toEqual([null, null, 120]); // 下载 rxr
    expect(s[2].data as (number | null)[]).toEqual([null, null, 30]); // 上传 txr
    const yAxis = opt.yAxis as { min: number; max: number }[];
    expect(yAxis[0].max).toBe(100);
  });
});

describe("sparklineOption", () => {
  it("微型线:无轴、线宽 1.5、平滑,数据透传", () => {
    const opt = sparklineOption({ data: [1, 2, 3], color: "#3b82f6" });
    const xAxis = opt.xAxis as { show: boolean };
    const yAxis = opt.yAxis as { show: boolean };
    expect(xAxis.show).toBe(false);
    expect(yAxis.show).toBe(false);
    const s = series0(opt);
    expect(s.data).toEqual([1, 2, 3]);
    const lineStyle = s.lineStyle as { width: number; color: string };
    expect(lineStyle.width).toBe(1.5);
    expect(lineStyle.color).toBe("#3b82f6");
  });

  it("空数据不抛错", () => {
    expect(() => sparklineOption({ data: [] })).not.toThrow();
    expect(series0(sparklineOption({ data: [] })).data).toEqual([]);
  });
});

describe("countdownGaugeOption", () => {
  it("全环倒计时:剩余秒数、总量、danger 渐变、800ms 动画", () => {
    const opt = countdownGaugeOption({ seconds: 30, total: 60 });
    const g = series0(opt);
    expect(g.startAngle).toBe(90);
    expect(g.endAngle).toBe(-270);
    expect(g.max).toBe(60);
    expect((g.data as { value: number }[])[0].value).toBe(30);
    expect(g.animationDuration).toBe(800);
    const progress = g.progress as { itemStyle: { color: { colorStops: { color: string }[] } } };
    expect(progress.itemStyle.color.colorStops[0].color).toBe("#f43f5e");
  });

  it("total 非法回退 1,seconds 负值夹 0", () => {
    const g = series0(countdownGaugeOption({ seconds: -5, total: 0 }));
    expect(g.max).toBe(1);
    expect((g.data as { value: number }[])[0].value).toBe(0);
  });
});

describe("barCompareOption", () => {
  it("横向 bar:类目在 y 轴、每条目可带独立颜色", () => {
    const opt = barCompareOption({
      items: [
        { label: "A", value: 10 },
        { label: "B", value: 20, color: "#3b82f6" },
      ],
      theme: light,
    });
    const yAxis = opt.yAxis as { data: string[] };
    expect(yAxis.data).toEqual(["A", "B"]);
    const s = series0(opt);
    const data = s.data as { value: number; itemStyle: { color: string } }[];
    expect(data).toHaveLength(2);
    expect(data[1].itemStyle.color).toBe("#3b82f6");
  });

  it("空 items 不抛错", () => {
    expect(() => barCompareOption({ items: [], theme: light })).not.toThrow();
  });
});

describe("defaultChartTheme / applyChartTheme tooltip 主题化", () => {
  it("surface 基线值:light #ffffff / dark #151c2c(取自 tokens.css --sa-surface)", () => {
    expect(light.surface).toBe("#ffffff");
    expect(dark.surface).toBe("#151c2c");
  });

  it("applyChartTheme 注入 tooltip 底色 surface / 边框 axis / 文字 text,并保留原有字段", () => {
    const opt = trendLineOption({ points: [], theme: light });
    const themed = applyChartTheme(opt, dark) as { tooltip: Record<string, unknown> };
    expect(themed.tooltip.backgroundColor).toBe(dark.surface);
    expect(themed.tooltip.borderColor).toBe(dark.axis);
    expect((themed.tooltip.textStyle as { color: string }).color).toBe(dark.text);
    expect(themed.tooltip.trigger).toBe("axis");
    expect(themed.tooltip.axisPointer).toEqual({ type: "cross" });
  });

  it("applyChartTheme 对不含 tooltip 的 option 不添加 tooltip 键", () => {
    const themed = applyChartTheme(sparklineOption({ data: [1, 2, 3] }), dark) as Record<
      string,
      unknown
    >;
    expect("tooltip" in themed).toBe(false);
  });
});

describe("EChart / HOC 在 jsdom 降级", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("EChart 无 canvas 时渲染 fallback 占位且不抛错", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const opt = gaugeOption({ value: 50, theme: light });
    const { container } = render(<EChart option={opt} className="h-40" />);
    expect(container.querySelector("[data-chart-fallback]")).not.toBeNull();
  });

  it("GaugeChart(含 useChartTheme + domain 渐变)在 jsdom 渲染 fallback 不抛错", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const { container } = render(<GaugeChart value={42} domain="monitor" className="h-40" />);
    expect(container.querySelector("[data-chart-fallback]")).not.toBeNull();
  });
});

describe("HeatBar / MetricBar 宽度与阈值色", () => {
  it("MetricBar:55 → 宽度 55% + amber(#f59e0b)", () => {
    const { container } = render(<MetricBar percent={55} />);
    const track = container.firstElementChild as HTMLElement;
    const fill = track.firstElementChild as HTMLElement;
    expect(fill.style.width).toBe("55%");
    expect(heatThresholdColor(55)).toBe("#f59e0b");
    expect(fill.style.backgroundColor).toBe("rgb(245, 158, 11)");
  });

  it("MetricBar:80 → 宽度 80% + rose(#f43f5e);显式 color 覆盖阈值色", () => {
    const { container } = render(<MetricBar percent={80} />);
    const fill = (container.firstElementChild as HTMLElement).firstElementChild as HTMLElement;
    expect(fill.style.width).toBe("80%");
    expect(heatThresholdColor(80)).toBe("#f43f5e");
    expect(fill.style.backgroundColor).toBe("rgb(244, 63, 94)");

    const override = render(<MetricBar percent={80} color="#3b82f6" />);
    const oFill = (override.container.firstElementChild as HTMLElement)
      .firstElementChild as HTMLElement;
    expect(oFill.style.backgroundColor).toBe("rgb(59, 130, 246)");
  });

  it("HeatBar:80 → 宽度 80% + rose;显示 label 与温度文本", () => {
    const { container, getByText } = render(<HeatBar value={80} label="PA" />);
    const track = container.querySelector(".bg-surface-3") as HTMLElement;
    const fill = track.firstElementChild as HTMLElement;
    expect(fill.style.width).toBe("80%");
    expect(fill.style.backgroundColor).toBe("rgb(244, 63, 94)");
    expect(getByText("PA")).toBeInTheDocument();
    expect(getByText("80°C")).toBeInTheDocument();
  });

  it("HeatBar 阈值边界:45 emerald / 46 amber / 60 amber / 61 orange / 75 orange / 76 rose", () => {
    expect(heatThresholdColor(45)).toBe("#10b981");
    expect(heatThresholdColor(46)).toBe("#f59e0b");
    expect(heatThresholdColor(60)).toBe("#f59e0b");
    expect(heatThresholdColor(61)).toBe("#f97316");
    expect(heatThresholdColor(75)).toBe("#f97316");
    expect(heatThresholdColor(76)).toBe("#f43f5e");
  });
});
