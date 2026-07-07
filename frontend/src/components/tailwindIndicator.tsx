import { cn } from "@/lib/utils";

export function TailwindIndicator() {

  return (
    <div className="fixed bottom-1 left-1 z-50 flex h-6 w-6 items-center justify-center rounded-full bg-foreground text-background p-3 font-mono text-xs">
      <div className="block sm:hidden">xs</div>
      <div className="hidden sm:block md:hidden">sm</div>
      <div className="hidden md:block lg:hidden">md</div>
      <div className="hidden lg:block xl:hidden">lg</div>
      <div className="hidden xl:block 2xl:hidden">xl</div>
      <div className="hidden 2xl:block">2xl</div>
    </div>
  );
}

export function SizeIndicator({ className }: { className?: string}) {

  return (
    <div className={cn("fixed w-full flex overflow-hidden justify-start", className)}>
      <div className="flex w-10 shrink-0 grow-0 gap-2">
        <div className="h-8 w-px shrink-0 bg-gray-950/20 dark:bg-white/30"></div>
        <span className="text-gray-950 dark:text-white">
          <div className="font-mono font-bold text-[13px]/7 text-gray-950 dark:text-white">xs</div>
        </span>
      </div>
      <div className="ml-150 flex w-32 shrink-0 grow-0 gap-2">
        <div className="h-8 w-px shrink-0 bg-gray-950/20 not-sm:bg-gray-950/5 dark:bg-white/30 dark:not-sm:bg-white/10"></div>
        <span className="not-sm:opacity-40">
          <div className="font-mono font-bold -ml-10 pr-4 text-right  text-[13px]/7 text-gray-950 dark:text-white">sm &gt; </div>
        </span>
      </div>
      <div className="flex w-64 shrink-0 grow-0 gap-2">
        <div className="h-8 w-px shrink-0 bg-gray-950/20 not-md:bg-gray-950/5 dark:bg-white/30 dark:not-md:bg-white/10"></div>
        <span className="not-md:opacity-40">
          <div className="font-mono font-bold -ml-10 pr-4 text-right  text-[13px]/7 text-gray-950 dark:text-white">md &gt;</div>
        </span>
      </div>
      <div className="flex w-64 shrink-0 grow-0 gap-2">
        <div className="h-8 w-px shrink-0 bg-gray-950/20 not-lg:bg-gray-950/5 dark:bg-white/30 dark:not-lg:bg-white/10"></div>
        <span className="not-lg:opacity-40">
          <div className="font-mono font-bold -ml-10 pr-4 text-right  text-[13px]/7 text-gray-950 dark:text-white">lg &gt;</div>
        </span>
      </div>
      <div className="flex w-64 shrink-0 grow-0 gap-2">
        <div className="h-8 w-px shrink-0 bg-gray-950/20 not-xl:bg-gray-950/5 dark:bg-white/30 dark:not-xl:bg-white/10"></div>
        <span className="not-xl:opacity-40">
          <div className="font-mono font-bold -ml-10 pr-4 text-right  text-[13px]/7 text-gray-950 dark:text-white">xl &gt;</div>
        </span>
      </div>
      <div className="flex w-64 shrink-0 grow-0 gap-2">
        <div className="h-8 w-px shrink-0 bg-gray-950/20 not-2xl:bg-gray-950/5 dark:bg-white/30 dark:not-2xl:bg-white/10"></div>
        <span className="not-2xl:opacity-40">
          <div className="font-mono font-bold -ml-12 pr-4 text-right  text-[13px]/7 text-gray-950 dark:text-white">2xl &gt;</div>
        </span>
      </div>
    </div>
  );
}
