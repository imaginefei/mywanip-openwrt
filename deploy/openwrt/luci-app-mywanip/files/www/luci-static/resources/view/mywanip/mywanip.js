'use strict';
'require view';
'require form';
'require uci';
'require poll';

return view.extend({
	load: function () {
		return uci.load('mywanip');
	},

	fetchStatus: function () {
		var port = uci.get('mywanip', 'main', 'port') || '9377';
		var url = window.location.protocol + '//' + window.location.hostname + ':' + port + '/';

		var el = document.getElementById('mywanip-status');
		if (!el) return Promise.resolve();

		return fetch(url, { mode: 'cors', signal: AbortSignal.timeout(3000) })
			.then(function (resp) { return resp.json(); })
			.then(function (data) {
				var v4 = data.ipv4 || '-';
				var v6 = data.ipv6 || '-';
				el.innerHTML =
					'<div style="padding: 10px 0;">' +
					'<div style="margin: 4px 0;">IPv4：<strong style="font-family: monospace;">' + v4 + '</strong></div>' +
					'<div style="margin: 4px 0;">IPv6：<strong style="font-family: monospace;">' + v6 + '</strong></div>' +
					'</div>';
			})
			.catch(function () {
				el.innerHTML =
					'<div style="padding: 10px 0; color: #c0392b;">' +
					_('服务未运行或不可达（请确认服务已启用，且 LuCI 不是通过 HTTPS 访问）') +
					'</div>';
			});
	},

	render: function () {
		var m, s, o;

		m = new form.Map('mywanip',
			_('外网IP查询'),
			_('HTTP 服务返回指定接口的 IPv4/IPv6 地址。接口路径：<code>/ipv4</code>、<code>/ipv6</code>（纯文本）与 <code>/</code>（JSON 汇总）。'));

		s = m.section(form.TypedSection, 'mywanip');
		s.anonymous = true;
		s.addremove = false;

		o = s.option(form.Flag, 'enabled', _('启用服务'));
		o.default = '0';
		o.rmempty = false;

		o = s.option(form.Value, 'interface', _('网络接口'),
			_('读取地址的接口设备名。PPPoE 拨号通常为 <code>pppoe-wan</code>，可在「网络 - 接口」或 <code>ifconfig</code> 中确认。'));
		o.default = 'pppoe-wan';
		o.rmempty = false;

		o = s.option(form.Value, 'port', _('HTTP 端口'),
			_('服务监听端口（1-65535），默认 9377。'));
		o.default = '9377';
		o.datatype = 'port';
		o.rmempty = false;

		o = s.option(form.Flag, 'bind_ipv4', _('绑定 IPv4'),
			_('在 IPv4 上监听（0.0.0.0）。'));
		o.default = '1';
		o.rmempty = false;

		o = s.option(form.Flag, 'bind_ipv6', _('绑定 IPv6'),
			_('在 IPv6 上监听（[::]）。'));
		o.default = '1';
		o.rmempty = false;

		return m.render().then(function (node) {
			var statusBox = E('div', { 'class': 'cbi-section' }, [
				E('h3', {}, _('当前状态（每 5 秒自动刷新）')),
				E('div', { id: 'mywanip-status' }, E('em', {}, _('加载中…')))
			]);

			poll.add(function () { return this.fetchStatus(); }.bind(this), 5);

			return E('div', {}, [statusBox, node]);
		}.bind(this));
	}
});
