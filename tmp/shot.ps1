# Capture the halo prototype's window. The pixel readback gfx does not have,
# done on the desktop: start the demo, find its RENDER window by title (a Go
# binary's MainWindowHandle is its console, not the window gogpu opened), send a
# key sequence, grab the window's client area off the screen, repeat, close it.
#
# Shots is one string, "name=keys;name=keys;...", because a bash caller cannot
# hand PowerShell an array.
param(
    [string]$Exe = "tmp\halo.exe",
    [string]$OutDir = "tmp\shots",
    [string]$Title = "cog examples: halo (prototype)",
    [string]$Shots = "default="
)

Add-Type -AssemblyName System.Drawing
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class Win {
    public delegate bool EnumProc(IntPtr h, IntPtr l);
    [DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc p, IntPtr l);
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowTextW(IntPtr h, System.Text.StringBuilder s, int n);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
    public static IntPtr FindByPid(uint want) {
        IntPtr found = IntPtr.Zero;
        EnumWindows(delegate(IntPtr h, IntPtr l) {
            uint pid; GetWindowThreadProcessId(h, out pid);
            if (pid != want || !IsWindowVisible(h)) return true;
            var sb = new System.Text.StringBuilder(512);
            GetWindowTextW(h, sb, sb.Capacity);
            var t = sb.ToString();
            if (t.Length == 0 || t.EndsWith(".exe")) return true;
            found = h; return false;
        }, IntPtr.Zero);
        return found;
    }
    [DllImport("user32.dll")] public static extern bool SetProcessDPIAware();
    [DllImport("user32.dll")] public static extern bool SetWindowPos(IntPtr h, IntPtr after, int x, int y, int cx, int cy, uint f);
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
    [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
    [DllImport("user32.dll")] public static extern void mouse_event(uint f, uint x, uint y, uint d, IntPtr e);
    [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr h, out RECT r);
    [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr h, ref POINT p);
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int L, T, R, B; }
    [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }
}
"@

# Without this the capture runs in virtualized coordinates and lands on the
# wrong pixels the moment the desktop is not at 100%.
[void][Win]::SetProcessDPIAware()
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$proc = Start-Process -FilePath $Exe -PassThru -WindowStyle Minimized

$h = [IntPtr]::Zero
for ($i = 0; $i -lt 40 -and $h -eq [IntPtr]::Zero; $i++) {
    Start-Sleep -Milliseconds 500
    $h = [Win]::FindByPid([uint32]$proc.Id)
}
if ($h -eq [IntPtr]::Zero) { Write-Error "no window titled '$Title'"; $proc.Kill(); exit 1 }
# Put it at the top left, on top of everything, before the first grab: a window
# with anything else over it captures the other window's pixels.
[void][Win]::SetWindowPos($h, [IntPtr](-1), 0, 0, 0, 0, 0x0041)
Start-Sleep -Seconds 3

# The client origin, taken once, so the focus click has somewhere to land.
$p0 = New-Object Win+POINT
[void][Win]::ClientToScreen($h, [ref]$p0)
$shell = New-Object -ComObject WScript.Shell
foreach ($shot in ($Shots -split ";")) {
    if (-not $shot) { continue }
    $name, $keys = $shot -split "=", 2
    # Every shot starts from the defaults, so one capture cannot inherit the
    # knobs another left behind.
    $keys = "{BS}," + $keys
    # SetForegroundWindow alone is refused often enough that the keys silently go
    # nowhere. A click in the window's own title bar takes focus every time, and
    # the demo reads no mouse at all.
    [void][Win]::SetForegroundWindow($h)
    [void][Win]::SetCursorPos($p0.X + 40, $p0.Y - 12)
    [Win]::mouse_event(0x0002, 0, 0, 0, [IntPtr]::Zero)
    [Win]::mouse_event(0x0004, 0, 0, 0, [IntPtr]::Zero)
    Start-Sleep -Milliseconds 500
    if ($keys) {
        # Comma-separated tokens rather than characters, so a token can be a
        # SendKeys escape like {BS}. One press per pause: a JustPressed edge
        # needs a frame of its own, and SendKeys types faster than 60Hz.
        foreach ($k in ($keys -split ",")) {
            if (-not $k) { continue }
            $shell.SendKeys($k)
            Start-Sleep -Milliseconds 250
        }
        Start-Sleep -Milliseconds 500
    }
    [void][Win]::SetForegroundWindow($h)
    Start-Sleep -Milliseconds 600

    $r = New-Object Win+RECT
    [void][Win]::GetClientRect($h, [ref]$r)
    $p = New-Object Win+POINT
    [void][Win]::ClientToScreen($h, [ref]$p)
    $w = $r.R - $r.L; $ht = $r.B - $r.T
    $bmp = New-Object System.Drawing.Bitmap $w, $ht
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.CopyFromScreen($p.X, $p.Y, 0, 0, $bmp.Size)
    $path = Join-Path $OutDir "$name.png"
    $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose(); $bmp.Dispose()
    Write-Output "$path ${w}x${ht} at $($p.X),$($p.Y)"
}

$proc.Kill()
