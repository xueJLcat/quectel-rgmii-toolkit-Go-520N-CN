(function (global) {
  'use strict';

  const root = global.SimpleAdmin || (global.SimpleAdmin = {});
  root.Vue = root.Vue || {};

  root.Vue.mount = function (factory, selector) {
    if (!global.Vue || typeof global.Vue.createApp !== 'function') {
      throw new Error('Vue 3 runtime is not loaded');
    }

    var source = typeof factory === 'function' ? factory() : (factory || {});
    var data = {};
    var methods = {};
    var computed = source.computed && typeof source.computed === 'object' ? source.computed : {};
    var watch = source.watch && typeof source.watch === 'object' ? source.watch : {};

    Object.keys(source).forEach(function (key) {
      if (key === 'computed' || key === 'watch') return;
      if (typeof source[key] === 'function') {
        methods[key] = source[key];
      } else {
        data[key] = source[key];
      }
    });

    var mountedApp = global.Vue.createApp({
      data: function () {
        return data;
      },
      methods: methods,
      computed: computed,
      watch: watch,
      mounted: function () {
        if (typeof this.init === 'function') {
          this.init();
        }
      }
    }).mount(selector || '#app');

    root.Vue.apps = root.Vue.apps || {};
    root.Vue.apps[selector || '#app'] = mountedApp;
    root.Vue.currentApp = mountedApp;
    global.__simpleadminApp = mountedApp;

    if (typeof global.dispatchEvent === 'function') {
      global.dispatchEvent(new CustomEvent('simpleadmin:vue-mounted', {
        detail: { selector: selector || '#app' }
      }));
    }

    return mountedApp;
  };

  global.mountSimpleAdminVueApp = root.Vue.mount;
})(window);
