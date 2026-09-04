(function (global) {
  'use strict';

  const root = global.SimpleAdmin || {};

  root.Logout = root.Logout || (function () {
    function start() {
      const finish = () => { global.location.replace('/login.html'); };
      if (global.fetch) {
        global.fetch('/api/logout', {
          method: 'POST',
          cache: 'no-store',
          credentials: 'same-origin'
        }).then(finish).catch(finish);
        return;
      }
      global.location.replace('/api/logout');
    }

    return { start };
  })();

  global.SimpleAdmin = root;
})(window);
