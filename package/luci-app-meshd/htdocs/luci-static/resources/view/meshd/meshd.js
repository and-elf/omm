'use strict';
'require view';

/*
 * Hosts the meshd PWA inside LuCI. The PWA is shipped as LuCI static resources
 * (view/meshd/pwa/) and served by uhttpd at the LuCI origin, so it works even
 * when meshd's management API is bound to localhost. We hand LuCI's session
 * token to the embedded app via the iframe URL hash; the PWA reads it and talks
 * to meshd through LuCI's authenticated /ubus endpoint (see web/src/api/ubus.ts
 * and the `meshd` rpcd object). Built with relative asset paths (vite base
 * './'), so serving from this subpath needs no separate build.
 */
return view.extend({
	load: function () { return Promise.resolve(); },
	handleSaveApply: null,
	handleSave: null,
	handleReset: null,

	render: function () {
		var token = (L && L.env && L.env.token) ? L.env.token : '';
		var src = L.resource('view/meshd/pwa/index.html') + '#ubus_token=' + encodeURIComponent(token);
		return E('iframe', {
			'src': src,
			'style': 'width:100%;height:80vh;border:0;background:#fff'
		});
	}
});
