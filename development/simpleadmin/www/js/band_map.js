// js/band_map.js
// 说明：非模块化写法，挂到 window，便于你现在的 defer 加载顺序直接使用
// 仅适配 RG520N-CN，频段为已确认的硬件能力值：
//   LTE  B1/3/5/8/34/38/39/40/41
//   NSA  n1/8/28/41/78
//   SA   n1/8/28/41/78
// 注意：本机固件 AT+QNWPREFCFG="ue_capability_band" 返回的是受限子集
// （LTE 仅 1:3:5:8、NSA 仅 78），不代表硬件能力，不得作为频段表依据。
// 设备型号恒定，不再保留任何型号识别/匹配逻辑。
(function (global) {
    // 目标模块：移远 RG520N-CN（硬编码，不再考虑其他设备兼容）。
    const TARGET_MODEL = "RG520N-CN";
    const DEFAULTS = {
        lte: "1:3:5:8:34:38:39:40:41",
        nsa: "1:8:28:41:78",
        sa: "1:8:28:41:78",
    };

    // 保留函数签名以兼容既有调用方（如 populate-checkbox.js、network.js）；
    // 实现改为无条件返回固定频段集的新副本，防止调用方修改污染共享对象。
    // 仅适配 RG520N-CN，忽略入参。
    function getBandsForModel(model, fallback = DEFAULTS) {
        return { ...DEFAULTS };
    }

    global.band_map = {
        TARGET_MODEL,
        getBandsForModel,
        DEFAULTS,
    };
})(window);

(function (global) {
    if (!global.SimpleAdmin) global.SimpleAdmin = {};
    global.SimpleAdmin.Bands = global.SimpleAdmin.Bands || {};
    global.SimpleAdmin.Bands.presets = global.band_map;
})(window);
