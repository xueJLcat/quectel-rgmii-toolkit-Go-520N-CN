(function (global) {
  'use strict';

  const root = global.SimpleAdmin || {};

  root.Text = root.Text || {
    lines(value) {
      return String(value || '')
        .split(/\r?\n/)
        .map((line) => line.trim())
        .filter(Boolean);
    },
    findStartsWith(lines, prefix) {
      return (lines || []).find((line) => line.startsWith(prefix));
    },
    findAllStartsWith(lines, prefix) {
      return (lines || []).filter((line) => line.startsWith(prefix));
    },
    splitCsv(value) {
      return (String(value || '').match(/"[^"]*"|[^,]+/g) || [])
        .map((item) => item.trim().replace(/^"|"$/g, ''));
    },
    hexToUtf16BE(hex) {
      const clean = String(hex || '').replace(/\s+/g, '');
      // UCS-2 十六进制必须按 4 位(一个码元)成组, 长度非 4 倍数说明
      // 末尾残缺, 直接判非法, 避免把两位残组解码成乱码字符。
      if (!clean || clean.length % 4 !== 0 || !/^[0-9a-fA-F]+$/.test(clean)) return '';
      let text = '';
      for (let i = 0; i < clean.length; i += 4) {
        const code = parseInt(clean.substr(i, 4), 16);
        if (!Number.isNaN(code)) text += String.fromCharCode(code);
      }
      return text;
    },
    compactHex(value) {
      return String(value || '').replace(/\s+/g, '');
    }
  };

  global.SimpleAdmin = root;
})(window);
