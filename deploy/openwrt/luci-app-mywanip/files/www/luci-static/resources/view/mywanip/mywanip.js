'use strict';
'require view';
'require form';
'require uci';
'require poll';
'require rpc';
'require ui';

// 通过 LuCI 标准 ubus 方法 luci.setInitAction 控制 init 脚本
// （rpcd-mod-luci 提供；等价于 /etc/init.d/<name> <action>）
var callInitAction = rpc.declare({
	object: 'luci',
	method: 'setInitAction',
	params: ['name', 'action'],
	expect: { result: false }
});

return view.extend({
	load: function () {
		return uci.load('mywanip');
	},

	serviceUrl: function (path) {
		var port = uci.get('mywanip', 'main', 'port') || '9377';
		return window.location.protocol + '//' + window.location.hostname + ':' + port + (path || '/');
	},

	// 调用 /etc/init.d/mywanipd <action>（enable/disable/start/stop/restart）
	initAction: function (action) {
		var view = this;
		return callInitAction('mywanipd', action).then(function () {
			ui.addNotification(null, E('p', _('操作已执行：%s').format(action)));
			setTimeout(function () { view.fetchStatus(); }, 1500);
		}).catch(function (e) {
			ui.addNotification(null, E('p', _('操作失败：%s').format(e || '无权限')));
		});
	},

	fetchStatus: function () {
		var url = this.serviceUrl('/');

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
				E('div', { id: 'mywanip-status' }, E('em', {}, _('加载中…'))),
				E('div', { 'style': 'margin: 12px 0 4px;' }, [
					E('button', {
						'class': 'btn cbi-button cbi-button-positive',
						'click': function (ev) {
							ev.preventDefault();
							// enable（开机自启）后 restart；要求页面已勾选「启用服务」并保存
							this.initAction('enable').then(function () {
								return this.initAction('restart');
							}.bind(this));
						}.bind(this)
					}, _('启动 / 重启服务')),
					' ',
					E('button', {
						'class': 'btn cbi-button cbi-button-negative',
						'click': function (ev) {
							ev.preventDefault();
							this.initAction('stop');
						}.bind(this)
					}, _('停止服务')),
					' ',
					E('a', {
						'class': 'btn cbi-button cbi-button-action',
						'href': this.serviceUrl('/'), 'target': '_blank', 'rel': 'noopener'
					}, _('在浏览器打开测试')),
					' ',
					E('a', {
						'class': 'btn cbi-button cbi-button-link',
						'href': this.serviceUrl('/ipv4'), 'target': '_blank', 'rel': 'noopener'
					}, '/ipv4'),
					' ',
					E('a', {
						'class': 'btn cbi-button cbi-button-link',
						'href': this.serviceUrl('/ipv6'), 'target': '_blank', 'rel': 'noopener'
					}, '/ipv6')
				]),
				E('p', { 'style': 'color: #888; margin: 4px 0;' },
					_('提示：修改配置后先「保存并应用」，再点「启动 / 重启服务」生效；「启动」会同时设置开机自启。'))
			]);

			poll.add(function () { return this.fetchStatus(); }.bind(this), 5);

			return E('div', {}, [statusBox, node]);
		}.bind(this));
	}
});
