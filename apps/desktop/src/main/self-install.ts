import { app, dialog } from 'electron'
import { spawn } from 'node:child_process'
import { existsSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { is } from '@electron-toolkit/utils'

// macOS 双击自安装:DMG 里只放 app,用户双击图标启动时,若发现自己不在
// /Applications,就把自己拷进去再重启 —— 免去"拖到 Applications"那一步。
//
// 为什么必须在 whenReady 的最前面调用(早于 ensureLocalServer):
//   首次启动时 app 跑在只读的 DMG 卷(甚至 App Translocation 的随机只读路径)。
//   若先拉起本地 server / Qdrant 再搬家,会从 DMG 路径 spawn 出子进程,搬家重启后
//   留下指向已卸载卷的孤儿进程。所以自安装是启动序列的第 0 步:要么搬家+重启,
//   要么原地继续 —— 二者必居其一,且在任何本地进程被拉起之前决定。

// 自安装弹框的界面文案跟随系统语言(app.getLocale),与 web 端 i18n 的
// en/zh/ja 三档对齐;其余语言回落英文。安装包流程先于登录/设置,读不到
// 应用内语言偏好,系统语言是这里唯一诚实的来源。
//
// 已知不一致(待菜单栏展开样式改版时同步):runningDetail 指引用户去点
// 菜单栏托盘图标的退出项,但托盘退出项在 index.ts buildTrayMenu 里
// 硬编码为英文 "Quit Memoh",zh/ja 文案给出的本地化名称与界面实际不符
// (en 恰好与硬编码一致)。改版引入快捷 Memo 等新面板项后,这里要按真实
// 退出项文案重新对齐。
type SelfInstallLocale = 'en' | 'zh' | 'ja'

interface SelfInstallStrings {
  runningTitle: string
  runningDetail: string
  conflictTitle: string
  conflictDetail: string
  replace: string
  keepBoth: string
  ok: string
}

const SELF_INSTALL_STRINGS: Record<SelfInstallLocale, SelfInstallStrings> = {
  en: {
    runningTitle: 'Quit Memoh to Install',
    runningDetail:
      'You are installing Memoh, but Memoh is still\n' +
      'running in the background. Quit Memoh to\n' +
      'continue the installation.\n\n' +
      'Closing the window does not quit Memoh. Click the\n' +
      'Memoh menu bar icon and choose "Quit Memoh",\n' +
      'then open this installer again.',
    conflictTitle: 'Memoh is already installed',
    conflictDetail:
      'A version of Memoh already exists in your Applications folder. Replace it with this one?',
    replace: 'Replace',
    keepBoth: 'Keep Both / Run Here',
    ok: 'OK',
  },
  zh: {
    runningTitle: '安装前请先退出 Memoh',
    runningDetail:
      '你正在安装 Memoh，但 Memoh 仍在后台运行。\n' +
      '请先退出 Memoh，再继续安装。\n\n' +
      '仅关闭窗口不会退出 Memoh。请点击菜单栏的\n' +
      'Memoh 图标，选择「退出 Memoh」，然后重新打开本安装包。',
    conflictTitle: '已安装 Memoh',
    conflictDetail: '「应用程序」文件夹中已存在一个 Memoh，要用当前版本替换它吗？',
    replace: '替换',
    keepBoth: '保留两者 / 原地运行',
    ok: '好',
  },
  ja: {
    runningTitle: 'インストールの前に Memoh を終了',
    runningDetail:
      'Memoh をインストールしようとしていますが、\n' +
      'Memoh はバックグラウンドで実行中です。\n' +
      'Memoh を終了してからインストールを続けてください。\n\n' +
      'ウインドウを閉じるだけでは終了しません。\n' +
      'メニューバーの Memoh アイコンから「Memoh を終了」を選び、\n' +
      'このインストーラーをもう一度開いてください。',
    conflictTitle: 'Memoh はすでにインストールされています',
    conflictDetail: 'アプリケーションフォルダにすでに Memoh があります。このバージョンで置き換えますか?',
    replace: '置き換える',
    keepBoth: '両方を残す / このまま実行',
    ok: 'OK',
  },
}

function selfInstallStrings(): SelfInstallStrings {
  // getLocale 不要求 ready:detached 提示(锁失败实例)赶在 ready 前调用,
  // 搬家冲突分支在 ready 后,两个入口都成立。
  const locale = app.getLocale().toLowerCase()
  if (locale.startsWith('zh')) return SELF_INSTALL_STRINGS.zh
  if (locale.startsWith('ja')) return SELF_INSTALL_STRINGS.ja
  return SELF_INSTALL_STRINGS.en
}

/**
 * 提示"旧版正在运行,先退出再装"。两个入口共用:
 * 1. index.ts 里没抢到单实例锁、被判定为安装/更新尝试的第二实例
 *    (仅带单实例锁的发行版存在此入口);
 * 2. 下方 moveToApplicationsFolder 的 existsAndRunning 冲突分支。
 */
export function showQuitRunningInstanceDialog(): void {
  const strings = selfInstallStrings()
  dialog.showMessageBoxSync({
    type: 'info',
    buttons: [strings.ok],
    defaultId: 0,
    cancelId: 0,
    title: strings.runningTitle,
    message: strings.runningTitle,
    detail: strings.runningDetail,
  })
}

function appleScriptString(value: string): string {
  return `"${value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`
}

/**
 * 用 osascript 弹同一个"请先退出"对话框 —— 供拿不到单实例锁的第二实例使用。
 *
 * 实测:经 LaunchServices 拉起的锁失败实例永远等不到 app ready(ready 被
 * second-instance 投递流程吞掉),而 Electron 的 dialog 在 ready 前不可用。
 * 所以提示交给独立的 osascript 进程显示,它不需要 ready,本进程退出后
 * 对话框仍然存活。
 */
export function showQuitRunningInstancePromptDetached(): void {
  const strings = selfInstallStrings()
  const icon = join(process.resourcesPath, 'icon.icns')
  // display dialog 不会自动按语义断行,文案里的 \n 逐行交给 AppleScript
  // 的 return 拼接,保证排版是写文案时刻意设计过的。
  const detail = strings.runningDetail
    .split('\n')
    .map(appleScriptString)
    .join(' & return & ')
  const script =
    `display dialog ${detail} ` +
    `with title ${appleScriptString(strings.runningTitle)} ` +
    `buttons {${appleScriptString(strings.ok)}} ` +
    `default button ${appleScriptString(strings.ok)}` +
    (existsSync(icon) ? ` with icon (POSIX file ${appleScriptString(icon)})` : '')
  try {
    spawn('/usr/bin/osascript', ['-e', script], {
      detached: true,
      stdio: 'ignore',
    }).unref()
  } catch (error) {
    console.error('self-install: failed to spawn prompt helper', error)
  }
}

function plistString(plist: string, key: string): string | null {
  const match = new RegExp(`<key>${key}</key>\\s*<string>\\s*([^<]+?)\\s*</string>`).exec(plist)
  return match?.[1] ?? null
}

// /Applications 里已装那一份的版本。未安装 / 不可读 / 非预期格式时返回 null。
function installedAppVersion(): string | null {
  try {
    const plist = readFileSync(`/Applications/${app.getName()}.app/Contents/Info.plist`, 'utf8')
    return plistString(plist, 'CFBundleShortVersionString') ?? plistString(plist, 'CFBundleVersion')
  } catch {
    return null
  }
}

/**
 * 当前进程是不是"从 /Applications 之外启动、与已装版本不同的打包副本"
 * —— 即一次被在运行的旧实例挡住的 DMG 安装/更新尝试。
 *
 * 供 index.ts 在没抢到单实例锁时区分:普通第二实例静默退出(深链已转交);
 * 安装尝试必须先提示用户退出旧版,否则更新意图被无声吞掉(秒退 + 旧窗口
 * 拉到前台,用户以为已经更新成功)。
 *
 * 已装版本读不到(null)按"不同"处理:此时旧实例同样挡着安装,提示仍是对的。
 */
export function shouldPromptRunningInstall(): boolean {
  if (process.platform !== 'darwin' || is.dev || !app.isPackaged) return false
  try {
    if (app.isInApplicationsFolder()) return false
  } catch {
    return false
  }
  return installedAppVersion() !== app.getVersion()
}

/**
 * 若在 macOS 打包态且当前不在 /Applications,尝试把 app 搬进 /Applications。
 *
 * @returns true 表示启动序列到此为止(搬家+重启,或提示后自行退出),
 *          调用方应立即 return、不要再启动任何本地进程。
 *          false 表示无需搬家 / 搬家失败 / 用户取消 —— 调用方按原地运行继续。
 */
export function maybeSelfInstallMacOS(): boolean {
  // 仅打包态的 macOS 才自安装。dev 下 app 路径本就不在 /Applications,不能动。
  if (process.platform !== 'darwin' || is.dev || !app.isPackaged) return false

  // 已在 /Applications(第二次及以后启动)-> 什么都不做,正常进。
  let alreadyInstalled = true
  try {
    alreadyInstalled = app.isInApplicationsFolder()
  } catch {
    // 某些沙盒/权限异常下该 API 可能抛;保守当作已安装,绝不阻塞启动。
    return false
  }
  if (alreadyInstalled) return false

  // existsAndRunning 冲突分支置位:弹过"请先退出"提示后本实例必须退出,
  // 不能原地运行。
  let promptedToQuit = false
  try {
    const moved = app.moveToApplicationsFolder({
      conflictHandler: (conflictType) => {
        // 已存在同名 app:让用户决定覆盖还是就地运行当前这份。
        // conflictType 取值是 'exists' / 'existsAndRunning'(Electron API 原文)。
        if (conflictType === 'exists') {
          const strings = selfInstallStrings()
          const response = dialog.showMessageBoxSync({
            type: 'question',
            buttons: [strings.replace, strings.keepBoth],
            defaultId: 0,
            cancelId: 1,
            title: strings.conflictTitle,
            message: strings.conflictTitle,
            detail: strings.conflictDetail,
          })
          // 返回 true = 继续搬家(丢弃旧版、装入这份);false = 放弃,原地运行。
          return response === 0
        }
        // 'existsAndRunning':旧版正在运行,无法安全覆盖 —— 提示用户退出旧版
        // 后重开安装包,本实例随即退出:原地继续跑会与指引自相矛盾(看似
        // 装完,实际跑的是 DMG 里的副本),还会再冒出一个并存实例。
        showQuitRunningInstanceDialog()
        promptedToQuit = true
        return false
      },
    })
    if (promptedToQuit) {
      app.quit()
      return true
    }
    // moved===true 时 Electron 会拷贝到 /Applications、启动那一份并退出当前实例。
    return moved
  } catch (error) {
    // 搬家失败(权限/磁盘/只读目标等)绝不能卡死首启 —— 吞掉,原地运行。
    console.error('self-install: moveToApplicationsFolder failed', error)
    return false
  }
}
