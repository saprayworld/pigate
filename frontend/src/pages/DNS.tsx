import React, { useState, useEffect } from "react";
import { systemService, type DNSConfig } from "@/services/systemService";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Globe, RefreshCw, AlertCircle, Check, Info, Server, Network } from "lucide-react";

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
    } catch (err: any) {
      setError(err.message || "Failed to load DNS settings.");
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    loadDNSConfig();
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
    } catch (err: any) {
      setError(err.message || "Failed to save DNS settings.");
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
    <div className="space-y-6">
      {/* 1. Header Section */}
      <div>
        <h1 className="text-3xl font-bold tracking-tight text-foreground flex items-center gap-2">
          <Globe className="h-7 w-7 text-primary fill-primary/10" />
          DNS Settings
        </h1>
        <p className="text-muted-foreground mt-1">
          ระบบจัดการ DNS Server และการตั้งค่า Domain Resolution ทั่วทั้งระบบ
        </p>
      </div>

      <div className="grid gap-6 md:grid-cols-3">
        {/* 2. DNS Settings Configuration Form */}
        <div className="md:col-span-2 space-y-4">
          <Card className="bg-card/25 border border-border/50 p-6 rounded-xl space-y-4">
            <h2 className="text-sm font-bold text-foreground uppercase tracking-wider flex items-center gap-1.5 border-b border-border/40 pb-2.5">
              <Server className="h-4 w-4 text-primary" /> System DNS Settings
            </h2>

            <form onSubmit={handleSave} className="space-y-5">
              {error && (
                <Alert variant="destructive" className="border-red-500/20 bg-red-500/5 py-2.5 px-3">
                  <AlertCircle className="h-4 w-4 text-red-400" />
                  <AlertDescription className="text-red-400 text-xs">{error}</AlertDescription>
                </Alert>
              )}

              {successMsg && (
                <Alert className="border-primary/20 bg-primary/5 py-2.5 px-3">
                  <Check className="h-4 w-4 text-primary" />
                  <AlertDescription className="text-primary text-xs">{successMsg}</AlertDescription>
                </Alert>
              )}

              {/* Mode Selection Toggle Buttons */}
              <div className="space-y-2">
                <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  DNS Configuration Mode
                </Label>
                <div className="flex rounded-lg border border-border bg-background p-0.5 gap-0.5 w-fit">
                  <button
                    type="button"
                    onClick={() => setMode("static")}
                    className={`px-4 py-1.5 text-xs font-bold rounded-md transition cursor-pointer ${
                      mode === "static"
                        ? "bg-primary text-primary-foreground"
                        : "text-muted-foreground hover:text-foreground hover:bg-muted/30"
                    }`}
                  >
                    Global Static DNS
                  </button>
                  <button
                    type="button"
                    onClick={() => setMode("wan")}
                    className={`px-4 py-1.5 text-xs font-bold rounded-md transition cursor-pointer ${
                      mode === "wan"
                        ? "bg-primary text-primary-foreground"
                        : "text-muted-foreground hover:text-foreground hover:bg-muted/30"
                    }`}
                  >
                    Use WAN DHCP DNS
                  </button>
                </div>
              </div>

              {/* Mode explanation */}
              <div className="bg-muted/10 border border-border/30 rounded-lg p-3 text-xs text-muted-foreground leading-relaxed flex gap-2">
                <Info className="h-4 w-4 text-primary shrink-0 mt-0.5" />
                {mode === "static" ? (
                  <span>
                    <strong>Global Static DNS:</strong> ปฏิเสธ DNS ที่ได้รับจาก ISP ทั้งหมด และบังคับระบบให้ใช้ DNS Server ที่กำหนดเองด้านล่างนี้
                  </span>
                ) : (
                  <span>
                    <strong>Use WAN DHCP DNS:</strong> ใช้ DNS Server ที่ได้รับมาแบบอัตโนมัติจากเราเตอร์ผู้ให้บริการ (WAN DHCP Lease) หากมีหลายการ์ด WAN จะเลือกใช้อันที่มีลำดับความสำคัญสูงสุด
                  </span>
                )}
              </div>

              {/* Local Domain Name */}
              <div className="space-y-1.5 pt-2">
                <Label htmlFor="local-domain" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  Local Domain Name <span className="text-red-500">*</span>
                </Label>
                <Input
                  id="local-domain"
                  type="text"
                  required
                  value={localDomain}
                  onChange={(e) => setLocalDomain(e.target.value)}
                  placeholder="pigate.local"
                  className="bg-background/50 h-9 font-mono text-sm max-w-sm"
                />
                <p className="text-[10px] text-muted-foreground mt-0.5">
                  โดเมนเนมระดับท้องถิ่นของระบบเครือข่ายภายใน (เช่น pigate.local หรือ home.lan)
                </p>
              </div>

              {/* Static DNS Fields */}
              {mode === "static" && (
                <div className="grid gap-4 sm:grid-cols-2 pt-2 animate-scale-up">
                  <div className="space-y-1.5">
                    <Label htmlFor="primary-dns" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                      Primary DNS Server <span className="text-red-500">*</span>
                    </Label>
                    <Input
                      id="primary-dns"
                      type="text"
                      required
                      value={primaryDns}
                      onChange={(e) => setPrimaryDns(e.target.value)}
                      placeholder="1.1.1.1"
                      className="bg-background/50 h-9 font-mono text-sm"
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="secondary-dns" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                      Secondary DNS Server <span className="text-red-500">*</span>
                    </Label>
                    <Input
                      id="secondary-dns"
                      type="text"
                      required
                      value={secondaryDns}
                      onChange={(e) => setSecondaryDns(e.target.value)}
                      placeholder="8.8.8.8"
                      className="bg-background/50 h-9 font-mono text-sm"
                    />
                  </div>
                </div>
              )}

              {/* Submit Buttons */}
              <div className="flex items-center justify-end pt-3 border-t border-border/40">
                <Button
                  type="submit"
                  disabled={isSaving}
                  className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/90 font-bold px-6"
                >
                  {isSaving ? (
                    <>
                      <RefreshCw className="h-4 w-4 animate-spin mr-2" />
                      กำลังบันทึก...
                    </>
                  ) : (
                    "Save Changes"
                  )}
                </Button>
              </div>
            </form>
          </Card>
        </div>

        {/* 3. Dynamically Obtained DNS Server list side-panel */}
        <div className="space-y-4">
          <Card className="bg-card/25 border border-border/50 p-6 rounded-xl space-y-4">
            <h2 className="text-sm font-bold text-foreground uppercase tracking-wider flex items-center gap-1.5 border-b border-border/40 pb-2.5">
              <Network className="h-4 w-4 text-indigo-400" /> Dynamic DNS List
            </h2>

            <p className="text-xs text-muted-foreground leading-relaxed">
              รายชื่อ DNS Server ที่ระบบได้รับแบบไดนามิกจากขา WAN ต่าง ๆ ผ่านโปรโตคอล DHCP
            </p>

            <div className="space-y-3 pt-2">
              {!config || !config.dynamicDnsServers || config.dynamicDnsServers.length === 0 ? (
                <div className="text-center py-6 text-xs text-muted-foreground italic border border-dashed border-border/50 rounded-lg">
                  ไม่มีข้อมูล DNS สำรองจากพอร์ต WAN
                </div>
              ) : (
                config.dynamicDnsServers.map((dyn, idx) => (
                  <div key={idx} className="bg-muted/10 border border-border/30 rounded-lg p-3 space-y-2 font-mono text-xs">
                    <div className="flex items-center justify-between border-b border-border/20 pb-1.5">
                      <span className="font-semibold text-foreground">{dyn.interfaceName}</span>
                      <span className="text-[10px] text-muted-foreground">({dyn.interfaceAlias})</span>
                    </div>
                    <div className="space-y-1">
                      {dyn.dnsServers.map((dns, dnsIdx) => (
                        <div key={dnsIdx} className="flex items-center gap-2 text-muted-foreground">
                          <Check className="h-3 w-3 text-indigo-400 shrink-0" />
                          <span>{dns}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                ))
              )}
            </div>
          </Card>
        </div>
      </div>
    </div>
  );
}
