(function (global) {
  'use strict';

  const root = global.SimpleAdmin || {};

  root.Sms = root.Sms || {
    parseConcatHeader(hex) {
      const normalized = root.Text.compactHex(hex).toUpperCase();
      if (!normalized || !/^[0-9A-F]+$/.test(normalized)) return null;
      if (normalized.startsWith('050003') && normalized.length > 12) {
        const total = parseInt(normalized.slice(8, 10), 16);
        const seq = parseInt(normalized.slice(10, 12), 16);
        if (total > 1 && seq >= 1 && seq <= total) {
          return { ref: normalized.slice(6, 8), total, seq, bodyHex: normalized.slice(12) };
        }
      }
      if (normalized.startsWith('060804') && normalized.length > 14) {
        const total = parseInt(normalized.slice(10, 12), 16);
        const seq = parseInt(normalized.slice(12, 14), 16);
        if (total > 1 && seq >= 1 && seq <= total) {
          return { ref: normalized.slice(6, 10), total, seq, bodyHex: normalized.slice(14) };
        }
      }
      return null;
    },
    decodeUcs2(hex) {
      return root.Text.hexToUtf16BE(hex);
    },
    encodeUcs2(text) {
      // 按 UTF-16 码元逐个编码: Array.from 会把增补平面字符合并成
      // 代理对字符串, charCodeAt(0) 只取到高代理项, 编出残缺码。
      const str = String(text || '');
      let hex = '';
      for (let i = 0; i < str.length; i++) {
        hex += str.charCodeAt(i).toString(16).padStart(4, '0').toUpperCase();
      }
      return hex;
    }
  };

  global.SimpleAdmin = root;
})(window);
