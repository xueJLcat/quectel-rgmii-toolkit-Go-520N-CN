// ECharts 按需注册的唯一入口:全站图表只从这里 import echarts 实例,严禁页面再引入完整 echarts。
// 仅注册实际用到的图表(Gauge/Line/Bar)、组件(Grid/Tooltip/Legend)、Canvas 渲染器,
// 以及可选特性 LabelLayout/UniversalTransition(后者为值更新提供平滑过渡,对应设计方案 §5.1)。
import * as echarts from "echarts/core";
import { BarChart, GaugeChart, LineChart } from "echarts/charts";
import { GridComponent, LegendComponent, TooltipComponent } from "echarts/components";
import { LabelLayout, UniversalTransition } from "echarts/features";
import { CanvasRenderer } from "echarts/renderers";
import type { ComposeOption } from "echarts/core";
import type { BarSeriesOption, GaugeSeriesOption, LineSeriesOption } from "echarts/charts";
import type {
  GridComponentOption,
  LegendComponentOption,
  TooltipComponentOption,
} from "echarts/components";

echarts.use([
  BarChart,
  GaugeChart,
  LineChart,
  GridComponent,
  LegendComponent,
  TooltipComponent,
  CanvasRenderer,
  LabelLayout,
  UniversalTransition,
]);

// 按需注册后收窄的 option 类型:只包含上面登记的 series/component,误用未注册项会在编译期报错。
export type EChartsOption = ComposeOption<
  | BarSeriesOption
  | GaugeSeriesOption
  | LineSeriesOption
  | GridComponentOption
  | LegendComponentOption
  | TooltipComponentOption
>;

export type { EChartsType } from "echarts/core";
export { echarts };
