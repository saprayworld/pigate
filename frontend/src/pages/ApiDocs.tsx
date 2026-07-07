import SwaggerUI from 'swagger-ui-react';
import 'swagger-ui-react/swagger-ui.css'; // สำคัญมาก ไม่งั้นหน้าตาจะพัง
import { useNavigate } from 'react-router-dom';
import { ArrowLeft, Flame, Moon, Sun } from 'lucide-react';
import { useTheme } from '@/hooks/useTheme';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';

function ApiDocs() {
    const navigate = useNavigate();
    const { theme, setTheme } = useTheme();

    return (
        <div className="min-h-screen bg-background text-foreground flex flex-col font-sans antialiased">
            {/* Premium Header */}
            <header className="sticky top-0 z-10 flex h-16 w-full items-center justify-between border-b border-border bg-background px-6">
                {/* Left Side: Brand & Back Navigation */}
                <div className="flex items-center gap-4">
                    <button
                        onClick={() => navigate('/dashboard')}
                        className="flex items-center gap-2 px-3 py-1.5 rounded-lg border border-border bg-card text-sm font-medium hover:bg-accent hover:text-accent-foreground transition duration-200 cursor-pointer"
                    >
                        <ArrowLeft className="h-4 w-4" />
                        <span>Dashboard</span>
                    </button>
                    
                    <div className="h-5 w-px bg-border hidden sm:block" />
                    
                    <div className="hidden sm:flex items-center gap-2">
                        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 border border-primary/20 text-primary">
                            <Flame className="h-4.5 w-4.5 fill-primary/20" />
                        </div>
                        <span className="text-lg font-bold tracking-wider">PiGate</span>
                        <Badge variant="outline" className="bg-primary/10 text-primary border border-primary/20 h-4.5 rounded-full px-1.5 text-[10px]">API Docs</Badge>
                    </div>
                </div>

                {/* Center Title (desktop) */}
                <h2 className="text-sm font-semibold tracking-tight text-foreground hidden md:block">
                    API Documentation
                </h2>

                {/* Right Side: Theme toggler */}
                <div className="flex items-center gap-3">
                    <div className="flex items-center gap-2 px-3 py-1.5 rounded-lg border border-border bg-card text-sm font-medium">
                        {theme === 'dark' ? (
                            <Moon className="w-4 h-4 text-primary" />
                        ) : (
                            <Sun className="w-4 h-4 text-amber-500" />
                        )}
                        <span className="text-xs hidden sm:inline">Dark Mode</span>
                        <Switch
                            checked={theme === 'dark'}
                            onCheckedChange={(checked) => setTheme(checked ? 'dark' : 'light')}
                            className="cursor-pointer"
                        />
                    </div>
                </div>
            </header>

            {/* Main Container */}
            <main className="flex-1 w-full max-w-7xl mx-auto px-4 py-8">
                <div className="bg-card border border-border rounded-xl p-4 md:p-8">
                    <SwaggerUI url="/openapi.yaml" />
                </div>
            </main>
        </div>
    );
}

export default ApiDocs;

