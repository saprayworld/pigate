import React, { useState, useEffect } from "react";
import { getErrorMessage } from "@/lib/errors";
import { systemService, type DNSConfig } from "@/services/systemService";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { RefreshCw, AlertCircle, Check, Info, Server, Network } from "lucide-react";

// Helper to validate IPv4 address format (0-255 octets)
const isValidIp = (ip: string): boolean => {
  const parts = ip.split(".");
  if (parts.length !== 4) return false;
  return parts.every((part) => {
    const num = parseInt(part, 10);
    return !isNaN(num) && num >= 0 && num <= 255 && part === num.toString();
  });
};

export default function DNS() {
  const [config, setConfig] = useState<DNSConfig | null>(null);
  const [mode, setMode] = useState<"wan" | "static">("static");
  const [primaryDns, setPrimaryDns] = useState("1.1.1.1");
  const [secondaryDns, setSecondaryDns] = useState("8.8.8.8");
  const [localDomain, setLocalDomain] = useState("pigate.local");

  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState("");
  const [successMsg, setSuccessMsg] = useState("");

  const loadDNSConfig = async () => {
    try {
      setIsLoading(true);
      setError("");
      const data = await systemService.getDNSConfig();
      setConfig(data);
      setMode(data.mode);
      setPrimaryDns(data.primaryDns);
      setSecondaryDns(data.secondaryDns);
      setLocalDomain(data.localDomain || "pigate.local");
    } catch (err) {
      setError(getErrorMessage(err) || "Failed to load DNS settings.");
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    // isLoading/error already start at their reset values; avoid a synchronous
    // setState in the effect body
    const initialLoad = async () => {
      try {
        const data = await systemService.getDNSConfig();
        setConfig(data);
        setMode(data.mode);
        setPrimaryDns(data.primaryDns);
        setSecondaryDns(data.secondaryDns);
        setLocalDomain(data.localDomain || "pigate.local");
      } catch (err) {
        setError(getErrorMessage(err) || "Failed to load DNS settings.");
      } finally {
        setIsLoading(false);
      }
    };
    initialLoad();
  }, []);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setSuccessMsg("");

    // Validate Local Domain Name
    if (!localDomain || localDomain.trim() === "") {
      setError("กรุณากรอก Local Domain Name");
      return;
    }
    const domainRegex = /^[a-zA-Z0-9.-]+$/;
    if (!domainRegex.test(localDomain)) {
      setError("Local Domain Name ไม่ถูกต้อง (รองรับเฉพาะ A-Z, a-z, 0-9, จุด . และขีด -)");
      return;
    }

    // Validate IPs if in static mode
    if (mode === "static") {
      if (!isValidIp(primaryDns)) {
        setError("กรุณากรอก Primary DNS IP Address ให้ถูกต้อง (เช่น 1.1.1.1)");
        return;
      }
      if (!isValidIp(secondaryDns)) {
        setError("กรุณากรอก Secondary DNS IP Address ให้ถูกต้อง (เช่น 8.8.8.8)");
        return;
      }
    }

    try {
      setIsSaving(true);
      await systemService.updateDNSConfig({
        mode,
        primaryDns: mode === "static" ? primaryDns : "",
        secondaryDns: mode === "static" ? secondaryDns : "",
        localDomain,
      });
      setSuccessMsg("บันทึกการตั้งค่า DNS เรียบร้อยแล้ว");
      // Reload config to get refreshed states
      await loadDNSConfig();
    } catch (err) {
      setError(getErrorMessage(err) || "Failed to save DNS settings.");
    } finally {
      setIsSaving(false);
    }
  };

  if (isLoading) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[400px] space-y-4">
        <RefreshCw className="h-8 w-8 animate-spin text-primary" />
        <span className="text-sm text-muted-foreground font-semibold">กำลังโหลดข้อมูล DNS...</span>
      </div>
    );
  }

  return (
    <div className="grid gap-4 md:grid-cols-3">
      {/* DNS Settings Configuration Form */}
      <Card className="md:col-span-2">
        <CardHeader className="space-y-0">
          <CardTitle className="flex items-center gap-2 text-base font-semibold">
            <Server className="h-4 w-4 text-muted-foreground" />
            System DNS Settings
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSave} className="space-y-5">
            {error && (
              <Alert variant="destructive" className="px-3 py-2.5">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription className="text-xs">{error}</AlertDescription>
              </Alert>
            )}

            {successMsg && (
              <Alert className="border-primary/20 bg-primary/5 px-3 py-2.5">
                <Check className="h-4 w-4 text-primary" />
                <AlertDescription className="text-xs text-primary">{successMsg}</AlertDescription>
              </Alert>
            )}

            {/* Mode Selection Toggle Buttons */}
            <div className="space-y-2">
              <Label className="block text-xs font-medium text-muted-foreground">
                DNS Configuration Mode
              </Label>
              <div className="flex w-fit gap-0.5 rounded-lg border border-border bg-muted p-0.5">
                <button
                  type="button"
                  onClick={() => setMode("static")}
                  className={`cursor-pointer rounded-md px-4 py-1.5 text-xs font-medium transition ${mode === "static"
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:bg-muted hover:text-foreground"
                    }`}
                >
                  Global Static DNS
                </button>
                {/* <button
                  type="button"
                  onClick={() => setMode("wan")}
                  className={`cursor-pointer rounded-md px-4 py-1.5 text-xs font-medium transition ${
                    mode === "wan"
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:bg-muted hover:text-foreground"
                  }`}
                >
                  Use WAN DHCP DNS
                </button> */}
              </div>
            </div>

            {/* Mode explanation */}
            <div className="flex gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs leading-relaxed text-muted-foreground">
              <Info className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
              {mode === "static" ? (
                <span>
                  <strong className="text-foreground">Global Static DNS:</strong> ปฏิเสธ DNS ที่ได้รับจาก ISP ทั้งหมด และบังคับระบบให้ใช้ DNS Server ที่กำหนดเองด้านล่างนี้
                </span>
              ) : (
                <span>
                  <strong className="text-foreground">Use WAN DHCP DNS:</strong> ใช้ DNS Server ที่ได้รับมาแบบอัตโนมัติจากเราเตอร์ผู้ให้บริการ (WAN DHCP Lease) หากมีหลายการ์ด WAN จะเลือกใช้อันที่มีลำดับความสำคัญสูงสุด
                </span>
              )}
            </div>

            {/* Local Domain Name */}
            <div className="space-y-1.5 pt-2">
              <Label htmlFor="local-domain" className="block text-xs font-medium text-muted-foreground">
                Local Domain Name <span className="text-destructive">*</span>
              </Label>
              <Input
                id="local-domain"
                type="text"
                required
                value={localDomain}
                onChange={(e) => setLocalDomain(e.target.value)}
                placeholder="pigate.local"
                className="h-9 max-w-sm font-mono text-sm"
              />
              <p className="mt-0.5 text-[10px] text-muted-foreground">
                โดเมนเนมระดับท้องถิ่นของระบบเครือข่ายภายใน (เช่น pigate.local หรือ home.lan)
              </p>
            </div>

            {/* Static DNS Fields */}
            {mode === "static" && (
              <div className="grid gap-4 pt-2 sm:grid-cols-2 animate-scale-up">
                <div className="space-y-1.5">
                  <Label htmlFor="primary-dns" className="block text-xs font-medium text-muted-foreground">
                    Primary DNS Server <span className="text-destructive">*</span>
                  </Label>
                  <Input
                    id="primary-dns"
                    type="text"
                    required
                    value={primaryDns}
                    onChange={(e) => setPrimaryDns(e.target.value)}
                    placeholder="1.1.1.1"
                    className="h-9 font-mono text-sm"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="secondary-dns" className="block text-xs font-medium text-muted-foreground">
                    Secondary DNS Server <span className="text-destructive">*</span>
                  </Label>
                  <Input
                    id="secondary-dns"
                    type="text"
                    required
                    value={secondaryDns}
                    onChange={(e) => setSecondaryDns(e.target.value)}
                    placeholder="8.8.8.8"
                    className="h-9 font-mono text-sm"
                  />
                </div>
              </div>
            )}

            {/* Submit Buttons */}
            <div className="flex items-center justify-end border-t border-border/50 pt-4">
              <Button type="submit" disabled={isSaving} className="cursor-pointer px-6 font-semibold">
                {isSaving ? (
                  <>
                    <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
                    กำลังบันทึก...
                  </>
                ) : (
                  "Save Changes"
                )}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      {/* Dynamically Obtained DNS Server list side-panel */}
      <Card>
        <CardHeader className="space-y-0">
          <CardTitle className="flex items-center gap-2 text-base font-semibold">
            <Network className="h-4 w-4 text-muted-foreground" />
            Dynamic DNS List
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-xs leading-relaxed text-muted-foreground">
            รายชื่อ DNS Server ที่ระบบได้รับแบบไดนามิกจากขา WAN ต่าง ๆ ผ่านโปรโตคอล DHCP
          </p>

          <div className="space-y-3">
            {!config || !config.dynamicDnsServers || config.dynamicDnsServers.length === 0 ? (
              <div className="rounded-lg border border-dashed border-border py-6 text-center text-xs italic text-muted-foreground">
                ไม่มีข้อมูล DNS สำรองจากพอร์ต WAN
              </div>
            ) : (
              config.dynamicDnsServers.map((dyn, idx) => (
                <div key={idx} className="space-y-2 rounded-lg border border-border bg-muted/50 p-3 font-mono text-xs">
                  <div className="flex items-center justify-between border-b border-border/50 pb-1.5">
                    <span className="font-semibold text-foreground">{dyn.interfaceName}</span>
                    <span className="text-[10px] text-muted-foreground">({dyn.interfaceAlias})</span>
                  </div>
                  <div className="space-y-1">
                    {dyn.dnsServers.map((dns, dnsIdx) => (
                      <div key={dnsIdx} className="flex items-center gap-2 text-muted-foreground">
                        <Check className="h-3 w-3 shrink-0 text-primary" />
                        <span>{dns}</span>
                      </div>
                    ))}
                  </div>
                </div>
              ))
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
