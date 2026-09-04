(function (global) {
  'use strict';

  const root = global.SimpleAdmin || {};

  root.Time = root.Time || {
    parseCustomDate(value) {
      // 时区既可能是两位刻度(+32)也可能是 时:分 形式(+08:00),都要剥离;
      // 旧正则只匹配两位结尾,带冒号的时区会让时间段切成 4 节而整体解析失败。
      const dateStr = String(value == null ? '' : value).replace(/[+-]\d{2}(:\d{2})?$/, '');
      const parts = dateStr.split(',');
      if (parts.length !== 2 || !parts[0] || !parts[1]) return null;
      const dateParts = parts[0].split('/').map(Number);
      const timeParts = parts[1].split(':').map(Number);
      if (dateParts.length !== 3 || timeParts.length !== 3) return null;
      if (dateParts.concat(timeParts).some((part) => !Number.isFinite(part))) return null;
      // 后端按 YY/MM/DD(年在前)下发,解构顺序必须与之一致,
      // 旧实现按 [day, month, year] 取值导致日月年错位。
      const [year, month, day] = dateParts;
      const [hour, minute, second] = timeParts;
      const date = new Date(Date.UTC(2000 + year, month - 1, day, hour, minute, second));
      return Number.isNaN(date.getTime()) ? null : date;
    },
    parseSmsDate(value) {
      return this.parseCustomDate(value) || new Date(NaN);
    },
    formatDateTime(date) {
      if (!date || typeof date.getUTCFullYear !== 'function' || Number.isNaN(date.getTime())) return '';
      const pad = (value) => value.toString().padStart(2, '0');
      return `${date.getUTCFullYear()}/${pad(date.getUTCMonth() + 1)}/${pad(date.getUTCDate())} ${pad(date.getUTCHours())}:${pad(date.getUTCMinutes())}:${pad(date.getUTCSeconds())}`;
    }
  };

  global.SimpleAdmin = root;
})(window);
