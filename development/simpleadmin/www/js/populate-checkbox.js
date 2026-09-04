(function (global) {
  'use strict';

  function bandList(value) {
    if (value === null || value === undefined) return [];
    return String(value)
      .split(':')
      .map(function (band) { return band.trim(); })
      .filter(function (band) { return band !== ''; });
  }

  function build(modelName, lockedBands) {
    const source = global.band_map
      ? global.band_map.getBandsForModel(modelName, global.band_map.DEFAULTS)
      : { lte: '', nsa: '', sa: '' };
    const locked = lockedBands || {};
    const defs = [
      { mode: 'LTE', title: 'LTE', prefix: 'B', value: source.lte, lockedValue: locked.LTE },
      { mode: 'NSA', title: 'NR5G-NSA', prefix: 'N', value: source.nsa, lockedValue: locked.NSA },
      { mode: 'SA', title: 'NR5G-SA', prefix: 'N', value: source.sa, lockedValue: locked.SA }
    ];
    return defs.map(function (def) {
      const lockedList = bandList(def.lockedValue);
      return {
        mode: def.mode,
        title: def.title,
        prefix: def.prefix,
        bands: bandList(def.value).map(function (name) {
          return { name: name, checked: lockedList.includes(name) };
        })
      };
    });
  }

  const root = global.SimpleAdmin || (global.SimpleAdmin = {});
  root.BandCheckboxes = {
    build,
    parse: bandList
  };
})(window);
