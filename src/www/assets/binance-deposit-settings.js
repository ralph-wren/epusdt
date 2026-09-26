import { r as interop } from "./chunk-DECur_0Z.js";
import { t as react } from "./react-CO2uhaBc.js";
import { wm as jsx } from "./messages-Cl9iE86Z.js";
import { t as Button } from "./button-DKoysvzZ.js";
import { t as Input } from "./input-BgIV5iqo.js";
import { t as PasswordInput } from "./password-input-BufJZJDT.js";
import { t as Switch } from "./switch-sN2XeEvz.js";
import { t as PageHeader } from "./page-header-IBw7XS2q.js";
import { o as useSettings, s as useSaveSettings, t as settingValue } from "./lib-DyUkl8FG.js";
import { n as toast } from "./dist-C1et795n.js";

const React = interop(react(), 1);
const ui = jsx();
const isChinese = (navigator.language || "").toLowerCase().startsWith("zh");
const label = (zh, en) => isChinese ? zh : en;

function BinanceDepositSettings() {
  const settings = useSettings("binance");
  const mutation = useSaveSettings();
  const [enabled, setEnabled] = React.useState(false);
  const [apiKey, setApiKey] = React.useState("");
  const [secretKey, setSecretKey] = React.useState("");
  const [pollInterval, setPollInterval] = React.useState("15");
  const [lookback, setLookback] = React.useState("30");
  const [apiConfigured, setApiConfigured] = React.useState(false);
  const [secretConfigured, setSecretConfigured] = React.useState(false);

  React.useEffect(() => {
    const rows = settings.data?.data;
    if (!rows) return;
    setEnabled(["true", "1"].includes(settingValue(rows, "binance.deposit_monitor_enabled").toLowerCase()));
    setPollInterval(settingValue(rows, "binance.poll_interval_seconds", "15"));
    setLookback(settingValue(rows, "binance.lookback_minutes", "30"));
    setApiConfigured(rows.find(row => row.key === "binance.api_key")?.configured === true);
    setSecretConfigured(rows.find(row => row.key === "binance.secret_key")?.configured === true);
    setApiKey("");
    setSecretKey("");
  }, [settings.data]);

  async function save(event) {
    event.preventDefault();
    const seconds = Number(pollInterval);
    const minutes = Number(lookback);
    if (!Number.isInteger(seconds) || seconds < 5 || seconds > 300 ||
        !Number.isInteger(minutes) || minutes < 1 || minutes > 1440) {
      toast.error(label("请检查轮询间隔和回看窗口", "Check the polling interval and lookback window"));
      return;
    }
    if (enabled && (!apiConfigured && !apiKey.trim() || !secretConfigured && !secretKey.trim())) {
      toast.error(label("启用前请填写 API Key 和 Secret Key", "Set both API Key and Secret Key before enabling"));
      return;
    }
    const items = [
      { group: "binance", key: "binance.deposit_monitor_enabled", type: "bool", value: enabled },
      { group: "binance", key: "binance.poll_interval_seconds", type: "int", value: seconds },
      { group: "binance", key: "binance.lookback_minutes", type: "int", value: minutes },
    ];
    if (apiKey.trim()) items.push({ group: "binance", key: "binance.api_key", type: "string", value: apiKey.trim() });
    if (secretKey.trim()) items.push({ group: "binance", key: "binance.secret_key", type: "string", value: secretKey.trim() });
    try {
      const response = await mutation.mutateAsync({ body: { items } });
      const failure = response.data?.find(row => !row.ok);
      if (failure || !response.data) {
        toast.error(label("保存失败，请检查配置", "Failed to save settings"));
        return;
      }
      setApiKey("");
      setSecretKey("");
      await settings.refetch();
      toast.success(label("保存成功", "Settings saved"));
    } catch {
      toast.error(label("保存失败，请稍后重试", "Failed to save settings"));
    }
  }

  const status = configured => label(configured ? "已配置" : "未配置", configured ? "Configured" : "Not configured");
  return ui.jsx(PageHeader, {
    title: label("币安入账监控", "Binance deposit monitor"),
    description: label("USDT 账户入账", "USDT account deposits"),
    variant: "section",
    children: ui.jsxs("form", {
      className: "space-y-6",
      onSubmit: save,
      children: [
        ui.jsxs("div", {
          className: "flex items-center justify-between gap-4",
          children: [
            ui.jsx("label", { htmlFor: "binance-enabled", className: "font-medium text-sm", children: label("启用入账监控", "Enable deposit monitoring") }),
            ui.jsx(Switch, { id: "binance-enabled", checked: enabled, onCheckedChange: setEnabled, disabled: settings.isLoading || mutation.isPending }),
          ],
        }),
        ui.jsxs("div", {
          className: "grid gap-6 md:grid-cols-2",
          children: [
            ui.jsxs("div", {
              className: "space-y-2",
              children: [
                ui.jsxs("div", { className: "flex justify-between gap-2 text-sm", children: [ui.jsx("label", { htmlFor: "binance-api-key", className: "font-medium", children: "API Key" }), ui.jsx("span", { className: "text-muted-foreground", children: status(apiConfigured) })] }),
                ui.jsx(PasswordInput, { id: "binance-api-key", autoComplete: "new-password", value: apiKey, onChange: event => setApiKey(event.target.value), disabled: settings.isLoading || mutation.isPending }),
              ],
            }),
            ui.jsxs("div", {
              className: "space-y-2",
              children: [
                ui.jsxs("div", { className: "flex justify-between gap-2 text-sm", children: [ui.jsx("label", { htmlFor: "binance-secret-key", className: "font-medium", children: "Secret Key" }), ui.jsx("span", { className: "text-muted-foreground", children: status(secretConfigured) })] }),
                ui.jsx(PasswordInput, { id: "binance-secret-key", autoComplete: "new-password", value: secretKey, onChange: event => setSecretKey(event.target.value), disabled: settings.isLoading || mutation.isPending }),
              ],
            }),
          ],
        }),
        ui.jsxs("div", {
          className: "grid gap-6 md:grid-cols-2",
          children: [
            ui.jsxs("div", { className: "space-y-2", children: [
              ui.jsx("label", { htmlFor: "binance-poll", className: "font-medium text-sm", children: label("轮询间隔（秒）", "Polling interval (seconds)") }),
              ui.jsx(Input, { id: "binance-poll", type: "number", min: 5, max: 300, step: 1, value: pollInterval, onChange: event => setPollInterval(event.target.value), disabled: settings.isLoading || mutation.isPending }),
            ] }),
            ui.jsxs("div", { className: "space-y-2", children: [
              ui.jsx("label", { htmlFor: "binance-lookback", className: "font-medium text-sm", children: label("回看窗口（分钟）", "Lookback window (minutes)") }),
              ui.jsx(Input, { id: "binance-lookback", type: "number", min: 1, max: 1440, step: 1, value: lookback, onChange: event => setLookback(event.target.value), disabled: settings.isLoading || mutation.isPending }),
            ] }),
          ],
        }),
        ui.jsx(Button, { type: "submit", disabled: settings.isLoading || mutation.isPending, children: label("保存", "Save") }),
      ],
    }),
  });
}

export { BinanceDepositSettings as component };
