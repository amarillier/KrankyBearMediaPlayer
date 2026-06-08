# Reusable Dialogs Guide

## Overview
The About and Help dialogs have been refactored into separate, reusable files that can be quickly copied to other Fyne projects. This follows the pattern from KrankyBearClock and makes it easy to maintain consistent branding across all applications.

---

## 📂 File Structure

```
gui/
├── about.go          ✨ Reusable About dialog
├── help.go           ✨ Reusable Help dialog
├── dialogs.go        ✨ Update checker dialog
├── theme.go          ✨ Theme switching (also reusable)
├── util.go           ✨ Update checker function
└── main.go           Integration point
```

---

## 🚀 Quick Copy to New Project

### Step 1: Copy Files
Copy these files to your new Fyne project:
```bash
# From this project to your new project
cp gui/about.go ../YourNewProject/
cp gui/help.go ../YourNewProject/
cp gui/theme.go ../YourNewProject/    # Optional: if you want theme switching
cp gui/util.go ../YourNewProject/     # Optional: if you want update checking
cp gui/dialogs.go ../YourNewProject/  # Optional: if you want update checking
```

### Step 2: Copy Assets
```bash
# Copy your icon
cp gui/assets/images/YourIcon.png ../YourNewProject/assets/images/
cp gui/assets/images/YourIcon64.png ../YourNewProject/assets/images/

# Generate bundled.go
cd ../YourNewProject
fyne bundle -o bundled.go assets/images/YourIcon.png
fyne bundle -o bundled.go -a assets/images/YourIcon64.png
```

### Step 3: Update Constants in Your main.go
```go
package main

import (
    "strconv"
    "time"
    
    "fyne.io/fyne/v2"
    "fyne.io/fyne/v2/app"
    "fyne.io/fyne/v2/driver/desktop"
)

const (
    appName    = "Your App Name"        // ← Change this
    appVersion = "1.0.0"                // ← Change this
    appAuthor  = "Your Name"            // ← Change this
    appID      = "com.yourdomain.app"   // ← Change this
)

var appCopyright = "Copyright © Your Name, 2024-" + strconv.Itoa(time.Now().Year())

func main() {
    myApp := app.NewWithID(appID)
    myWindow := myApp.NewWindow(appName)
    
    // Load theme
    loadTheme(myApp)
    
    // Set icon (update resource name to match your bundled icon)
    myWindow.SetIcon(resourceYourIconPng)  // ← Update resource name
    
    // Create menus
    createMenus(myApp, myWindow)
    
    // Setup system tray (optional)
    if desk, ok := myApp.(desktop.App); ok {
        setupSystemTray(desk, myApp, myWindow)
    }
    
    // Your app content here...
    
    myWindow.ShowAndRun()
}

// Copy createMenus and setupSystemTray from main.go
// (See "Menu Integration" section below)
```

---

## 📝 Customization Guide

### About Dialog (`about.go`)

**What to customize:**

1. **Application Description** (line ~48):
```go
description := widget.NewLabel("A comprehensive Template utility")
// Change to your app's description:
description := widget.NewLabel("Your application description here")
```

2. **GitHub URL** (line ~62):
```go
licenseURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer/blob/allanm/LICENSE")
githubURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer")
// Change to your repository:
licenseURL, _ := url.Parse("https://github.com/yourusername/yourrepo/blob/allanm/LICENSE")
githubURL, _ := url.Parse("https://github.com/yourusername/yourrepo")
```

3. **Icon Resource** (line ~26 and others):
```go
aboutWindow.SetIcon(resourceKrankyBearBeanieMultiColorPng)
icon := canvas.NewImageFromResource(resourceKrankyBearBeanieMultiColorPng)
// Change to your embedded resource:
aboutWindow.SetIcon(resourceYourIconPng)
icon := canvas.NewImageFromResource(resourceYourIconPng)
```

4. **Window Size** (line ~80, optional):
```go
aboutWindow.Resize(fyne.NewSize(450, 500))
// Adjust if needed for your content
```

**What automatically adapts:**
- ✅ `appName` from constants
- ✅ `appVersion` from constants
- ✅ `appAuthor` from constants
- ✅ `appCopyright` from constants

---

### Help Dialog (`help.go`)

**What to customize:**

1. **Help Text** (line ~31 - major section):
```go
helpText := `Your App Name - Comprehensive Guide

SECTION 1:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Feature 1
  - Detail 1
  - Detail 2

• Feature 2
  - Detail 1
  - Detail 2

TIPS & TRICKS:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
💡 Tip 1...
💡 Tip 2...

KEYBOARD SHORTCUTS:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Cmd/Ctrl+Q - Quit
• Your shortcuts here...
`
```

**Tips for good help text:**
- Use visual separators (━━━) for sections
- Include emoji icons (💡 ✨ ⚠️ 📦) for visual interest
- Group related features together
- Add tips, tricks, and known limitations
- Include keyboard shortcuts
- Provide links to external assets

2. **GitHub and License URLs** (line ~172-176):
```go
githubURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer")
licenseURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer/blob/allanm/LICENSE")
// Change to your repository
```

3. **Icon Resource** (line ~25):
```go
helpWindow.SetIcon(resourceKrankyBearBeanieMultiColorPng)
// Change to your embedded resource
```

4. **Window Size** (line ~185, optional):
```go
helpWindow.Resize(fyne.NewSize(700, 600))
// Adjust based on your help content length
```

**What automatically adapts:**
- ✅ `appName` in title from constants

---

### Update Checker (`dialogs.go` + `util.go`)

**util.go - What to customize:**

1. **Repository Info** (line ~10-14):
```go
uc := updatechecker.New(
    "amarillier",                      // ← Your GitHub username
    "KrankyBearMediaPlayer",                   // ← Your repo name
    "Kranky Bear Template Utility",         // ← Your app display name
    "https://github.com/amarillier/KrankyBearMediaPlayer/releases/latest",  // ← Your releases URL
    0,
    false,
)
```

**dialogs.go - What to customize:**

1. **URLs** (lines with url.Parse):
```go
releaseURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer/releases/latest")
notesURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer/blob/allanm/ReleaseNotes.txt")
// Change to your repository
```

2. **Icon Resource**:
```go
updateWindow.SetIcon(resourceKrankyBearBeanieMultiColorPng)
icon := canvas.NewImageFromResource(resourceKrankyBearBeanieMultiColorPng)
// Change to your embedded resource
```

---

### Theme System (`theme.go`)

**Usually no customization needed!** The theme system is fully generic.

**Optional customization:**

If you want custom theme behavior (e.g., larger headings), uncomment and modify:

```go
type appTheme struct {
    fyne.Theme
}

func (a *appTheme) Size(n fyne.ThemeSizeName) float32 {
    if n == theme.SizeNameHeadingText {
        return a.Theme.Size(n) * 1.5  // ← Adjust multiplier
    }
    return a.Theme.Size(n)
}

// Then in main.go:
a.Settings().SetTheme(&appTheme{Theme: theme.DefaultTheme()})
```

---

## 🔗 Menu Integration

Add these functions to your `main.go`:

### createMenus Function
```go
func createMenus(a fyne.App, w fyne.Window) {
    // File menu
    quitItem := fyne.NewMenuItem("Quit", func() {
        a.Quit()
    })
    fileMenu := fyne.NewMenu("File", quitItem)

    // Help menu
    aboutItem := fyne.NewMenuItem("About", func() {
        showAbout(a)
    })
    helpItem := fyne.NewMenuItem("Help", func() {
        showHelp(a)
    })
    updateItem := fyne.NewMenuItem("Check for Updates", func() {
        message, available := checkForUpdates()
        showUpdateDialog(a, message, available)
    })
    helpMenu := fyne.NewMenu("Help", aboutItem, updateItem, helpItem)

    // Settings menu (if using theme.go)
    lightThemeItem := fyne.NewMenuItem("Light Theme", func() {
        setLightTheme(a)
    })
    darkThemeItem := fyne.NewMenuItem("Dark Theme", func() {
        setDarkTheme(a)
    })
    systemThemeItem := fyne.NewMenuItem("System Theme", func() {
        setSystemTheme(a)
    })
    settingsMenu := fyne.NewMenu("Settings", lightThemeItem, darkThemeItem, systemThemeItem)

    // Main menu
    mainMenu := fyne.NewMainMenu(fileMenu, helpMenu, settingsMenu)
    w.SetMainMenu(mainMenu)
}
```

### setupSystemTray Function (Optional)
```go
func setupSystemTray(desk desktop.App, a fyne.App, w fyne.Window) {
    showItem := fyne.NewMenuItem("Show", func() {
        w.Show()
        w.RequestFocus()
    })
    hideItem := fyne.NewMenuItem("Hide", func() {
        w.Hide()
    })
    aboutItem := fyne.NewMenuItem("About", func() {
        showAbout(a)
    })
    helpItem := fyne.NewMenuItem("Help", func() {
        showHelp(a)
    })
    updateItem := fyne.NewMenuItem("Check for Updates", func() {
        message, available := checkForUpdates()
        showUpdateDialog(a, message, available)
    })
    quitItem := fyne.NewMenuItem("Quit", func() {
        a.Quit()
    })

    menu := fyne.NewMenu(appName,
        showItem,
        hideItem,
        fyne.NewMenuItemSeparator(),
        aboutItem,
        helpItem,
        updateItem,
        fyne.NewMenuItemSeparator(),
        quitItem,
    )

    desk.SetSystemTrayMenu(menu)
    desk.SetSystemTrayIcon(resourceYourIconPng)  // ← Update resource name
}
```

---

## 📦 Dependencies

Add to your `go.mod`:

```go
require (
    fyne.io/fyne/v2 v2.5.4
    fyne.io/systray v1.11.0                          // For system tray
    github.com/amarillier/go-update-checker v0.0.3   // For update checking
)
```

Then run:
```bash
go mod tidy
```

---

## ✅ Checklist for New Project

### Initial Setup
- [ ] Copy `about.go`, `help.go` to your project
- [ ] Copy `theme.go` (optional, but recommended)
- [ ] Copy `util.go` and `dialogs.go` (optional, for update checking)
- [ ] Copy your icon to `assets/images/`
- [ ] Run `fyne bundle` to create `bundled.go`

### Customization
- [ ] Update `appName` constant in `main.go`
- [ ] Update `appVersion` constant in `main.go`
- [ ] Update `appAuthor` constant in `main.go`
- [ ] Update `appID` constant in `main.go`
- [ ] Update `appCopyright` variable in `main.go`

### about.go
- [ ] Change app description (line ~48)
- [ ] Update GitHub URL (line ~62)
- [ ] Update License URL (line ~60)
- [ ] Update icon resource name throughout file

### help.go
- [ ] Write comprehensive help text for your app (line ~31)
- [ ] Update GitHub URL
- [ ] Update License URL
- [ ] Update icon resource name

### util.go (if using)
- [ ] Update GitHub username (line ~10)
- [ ] Update repository name (line ~11)
- [ ] Update app display name (line ~12)
- [ ] Update releases URL (line ~13)

### dialogs.go (if using)
- [ ] Update releases URL (line with releaseURL)
- [ ] Update release notes URL (line with notesURL)
- [ ] Update icon resource name

### Integration
- [ ] Add `createMenus()` function to `main.go`
- [ ] Add `setupSystemTray()` function to `main.go` (optional)
- [ ] Call `loadTheme(myApp)` in `main()`
- [ ] Call `createMenus(myApp, myWindow)` in `main()`
- [ ] Call `setupSystemTray()` if desktop app in `main()`

### Testing
- [ ] Build: `go build`
- [ ] Run and test About dialog
- [ ] Run and test Help dialog
- [ ] Run and test Update checker (if added)
- [ ] Test theme switching (if added)
- [ ] Test system tray (if added)

---

## 🎨 Customization Examples

### Example 1: Minimal Setup (Just About & Help)

**Files needed:**
- `about.go`
- `help.go`
- `bundled.go` (generated)
- `YourIcon.png`

**In main.go:**
```go
const (
    appName    = "My Cool App"
    appVersion = "1.0.0"
    appAuthor  = "Jane Developer"
    appID      = "com.example.mycoolapp"
)

var appCopyright = "Copyright © Jane Developer, 2025"

func main() {
    myApp := app.NewWithID(appID)
    myWindow := myApp.NewWindow(appName)
    myWindow.SetIcon(resourceMyIconPng)
    
    // Simple menu
    aboutItem := fyne.NewMenuItem("About", func() { showAbout(myApp) })
    helpItem := fyne.NewMenuItem("Help", func() { showHelp(myApp) })
    quitItem := fyne.NewMenuItem("Quit", func() { myApp.Quit() })
    
    menu := fyne.NewMainMenu(
        fyne.NewMenu("File", quitItem),
        fyne.NewMenu("Help", aboutItem, helpItem),
    )
    myWindow.SetMainMenu(menu)
    
    // Your app content...
    
    myWindow.ShowAndRun()
}
```

### Example 2: Full Setup (Everything)

**Files needed:**
- `about.go`
- `help.go`
- `theme.go`
- `util.go`
- `dialogs.go`
- `bundled.go` (generated)
- `YourIcon.png`
- `YourIcon64.png`

**Result:** Complete professional app with About, Help, Update checking, Theme switching, and System tray!

---

## 🔍 Finding and Replacing

### Quick Find/Replace in Your Editor

When you copy the files, do a project-wide find/replace:

1. **Icon Assets:**
   - Find: `resourceKrankyBearBeanieMultiColorPng`
   - Replace: `resourceYourIconPng`

2. **URLs:**
   - Find: `github.com/amarillier/KrankyBearMediaPlayer`
   - Replace: `github.com/yourusername/yourrepo`

3. **Description (in about.go):**
   - Find: `"A comprehensive Template management utility"`
   - Replace: `"Your app description"`

---

## 💡 Tips for Great Help Content

### Structure
```
APP NAME - Brief Description

SECTION 1: Core Features
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Feature 1
  - How to use
  - Important notes
  
SECTION 2: Advanced Features
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
...

TIPS & TRICKS:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
💡 Tip 1
💡 Tip 2

KNOWN LIMITATIONS:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  Limitation 1
⚠️  Limitation 2

KEYBOARD SHORTCUTS:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Shortcut list

MORE INFORMATION:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Links to external assets
```

### Visual Elements
Use Unicode characters for visual appeal:
- Separators: `━━━` (U+2501)
- Bullets: `•` (U+2022)
- Arrows: `→` (U+2192)
- Emojis: `✨ 💡 ⚠️ 📦 🎯 ✓ ✅`

---

## 📚 Additional Assets

### Related Files in This Project
- `GUI-PROFESSIONAL-FEATURES.md` - Complete feature documentation
- `GUI-IMPLEMENTATION-COMPLETE.md` - Implementation summary
- `GUI-VERIFICATION-CHECKLIST.md` - Testing guide

### Fyne Documentation
- Dialogs: https://developer.fyne.io/dialog/
- Menus: https://developer.fyne.io/menuitem/
- Icons: https://developer.fyne.io/api/v2/canvas/image.html
- Bundling: https://developer.fyne.io/started/packaging

---

## 🎉 Result

With these reusable files, you can add professional About, Help, Theme switching, and Update checking to any Fyne application in **less than 30 minutes**!

Just:
1. ✅ Copy files
2. ✅ Update constants
3. ✅ Customize content (help text, URLs)
4. ✅ Update icon resource names
5. ✅ Add menu integration
6. ✅ Done!

**Consistent branding across all your applications! 🚀**

---

*"Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." — Winston Churchill, November 10, 1942*

