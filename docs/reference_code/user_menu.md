
```tsx
<DropdownMenu>
  <DropdownMenuTrigger asChild>
    <Button variant="ghost" size="icon" className="rounded-full bg-muted/50 ml-1 cursor-pointer relative overflow-hidden">
      {session.user.image ? (
        <img src={session.user.image} alt="User profile" className="w-full h-full object-cover" />
      ) : (
        <UserIcon className="w-4 h-4" />
      )}
    </Button>
  </DropdownMenuTrigger>
  <DropdownMenuContent align="end" className="w-56">
    <DropdownMenuLabel>
      <div className="flex flex-col space-y-1">
        <p className="text-sm font-medium leading-none">{session.user.name}</p>
        <p className="text-xs leading-none text-muted-foreground">{session.user.email}</p>
      </div>
    </DropdownMenuLabel>
    <DropdownMenuSeparator />
    <DropdownMenuGroup>
      <DropdownMenuItem asChild className="cursor-pointer">
        <Link href="/profile">
          <UserIcon className="w-4 h-4 mr-2" />
          {t('userMenu.profile')}
        </Link>
      </DropdownMenuItem>
      <DropdownMenuItem asChild className="cursor-pointer">
        <Link href="/kanban/report">
          <ChartPie className="w-4 h-4 mr-2" />
          {t('userMenu.report')}
        </Link>
      </DropdownMenuItem>
      <DropdownMenuItem asChild className="cursor-pointer">
        <Link href="/kanban/setting">
          <Settings className="w-4 h-4 mr-2" />
          {t('userMenu.setting')}
        </Link>
      </DropdownMenuItem>
    </DropdownMenuGroup>

    {/* Appearance section — แสดงเฉพาะบนมือถือ */}
    <div className="">
      <DropdownMenuSeparator />
      <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
        {t('userMenu.appearance')}
      </DropdownMenuLabel>
      <DropdownMenuItem
        onSelect={(e) => {
          e.preventDefault();
          setTheme(isDarkMode ? "light" : "dark");
        }}
        className="cursor-pointer"
      >
        {isDarkMode ? (
          <Moon className="w-4 h-4 mr-2" />
        ) : (
          <Sun className="w-4 h-4 mr-2" />
        )}
        <span className="flex-1">{t('userMenu.darkMode')}</span>
        <div
          className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors ${isDarkMode ? 'bg-background' : 'bg-muted-foreground/30'
            }`}
        >
          <span
            className={`inline-block h-3.5 w-3.5 rounded-full bg-white shadow-sm transition-transform ${isDarkMode ? 'translate-x-[18px]' : 'translate-x-[3px]'
              }`}
          />
        </div>
      </DropdownMenuItem>
      <DropdownMenuSub>
        <DropdownMenuSubTrigger className="cursor-pointer">
          <Globe className="w-4 h-4 mr-2" />
          {t('userMenu.language')}
        </DropdownMenuSubTrigger>
        <DropdownMenuSubContent>
          <DropdownMenuItem
            onClick={() => changeLanguage('th')}
            className={`cursor-pointer ${locale === 'th' ? 'bg-muted' : ''}`}
            disabled={isPending}
          >
            {locale === 'th' && <Check className="w-4 h-4 mr-2" />}
            <span className={locale !== 'th' ? 'ml-6' : ''}>ไทย (TH)</span>
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => changeLanguage('en')}
            className={`cursor-pointer ${locale === 'en' ? 'bg-muted' : ''}`}
            disabled={isPending}
          >
            {locale === 'en' && <Check className="w-4 h-4 mr-2" />}
            <span className={locale !== 'en' ? 'ml-6' : ''}>English (EN)</span>
          </DropdownMenuItem>
        </DropdownMenuSubContent>
      </DropdownMenuSub>
    </div>


    <DropdownMenuSeparator />
    <DropdownMenuItem onSelect={() => setIsAboutOpen(true)} className="cursor-pointer">
      <Info className="w-4 h-4 mr-2" />
      {t('userMenu.about')}
    </DropdownMenuItem>

    <DropdownMenuSeparator />
    <DropdownMenuItem
      onSelect={(e) => {
        e.preventDefault(); // ป้องกันไม่ให้ Dropdown ปิดทันทีเพื่อให้ผู้ใช้เห็นสถานะ Loading
        handleLogout();
      }}
      className="text-destructive cursor-pointer data-[disabled]:opacity-50"
      disabled={isLoggingOut}
    >
      {isLoggingOut ? (
        <Loader2 className="w-4 h-4 mr-2 animate-spin" />
      ) : (
        <LogOut className="w-4 h-4 mr-2" />
      )}
      {isLoggingOut ? t('userMenu.loggingOut') : t('userMenu.logout')}
    </DropdownMenuItem>

  </DropdownMenuContent>
</DropdownMenu>
```
