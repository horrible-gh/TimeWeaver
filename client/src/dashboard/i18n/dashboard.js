// src/i18n.js
import { createI18n } from 'vue-i18n'
import ko_msg from './ko/dashboard'

const userLocale = navigator.language.split('-')[0] || 'en';

// Define only the message objects for each language.
const messages = {
  en: {
    sub_dashboard: 'Dashboard',
    sub_devices: "Devices",
    sub_schedules: "Manage Schedules",
    sub_history: "Schedules History",
    sub_groups: "Groups",
    sub_users: "Users",
    sub_logout:"Logout",

    chart_title: "Service",
    chart_subtitle_device: "Devices Status",
    chart_devices_content1: "Active",
    chart_devices_content2: "Error",
    chart_devices_content3: "Inactive",
    chart_devices_content_title: "Devices",
    chart_subtitle_schedules: "Schedules Status",
    chart_schedules_content1: "Active",
    chart_schedules_content2: "Error",
    chart_schedules_content3: "Inactive",
    chart_schedules_content_title: "Schedules",
    chart_subtitle_tasks: "Tasks Status",
    chart_tasks_content_title1: "Running",
    chart_tasks_content_title2: "Wait",
    chart_tasks_content_title3: "Complate",
    chart_tasks_content_title4: "Error",
    chart_last_updated_time_title: "Last Update Time",
    schedules_list_no_datas: "No Datas",
    select_box_all: "ALL",

    list_label_group: "Group",
    list_label_schedule_name: "Schedule Name",
    list_label_start_time: "Start Time",
    list_label_end_time: "End Time",
    list_label_exit_code: "Exit Code",
    list_label_message: "Message",
    list_label_status: "Status",
    list_label_version: "Version",
    list_label_actions: "Actions",
    list_label_device: "Device",
    list_label_last_login_at: "Last Login",

    label_active: "Active",
    label_inactive: "Inactive",

    btn_filter_reset: "Reset",
    btn_close: "Close",
    btn_prev: "Previous",
    btn_next: "Next",
    btn_edit: "Edit",
    btn_remove: "Remove",
    btn_add: "Add",
    btn_save: "Save",

    msg_enter_group_name: "Enter Group Name",
    msg_delete_group_name: "Are you sure you want to delete the group?",
  },
  ja: {
    sub_dashboard: 'ダッシュボード',
    sub_devices: "デバイス管理",
    sub_schedules: "スケジュール管理",
    sub_tasks: "タスク管理",
    sub_history: "スケジュール履歴",
    sub_groups: "グループ管理",
    sub_users: "ユーザー管理",
    sub_logout:"ログアウト",

    chart_title: "サービス",
    chart_subtitle_device: "デバイス状況",
    chart_devices_content1: "活性",
    chart_devices_content2: "エラー",
    chart_devices_content3: "非活動",
    chart_devices_content_title: "",
    chart_subtitle_schedules: "スケジュール状況",
    chart_schedules_content1: "活性",
    chart_schedules_content2: "エラー",
    chart_schedules_content3: "非活動",
    chart_schedules_content_title: "スケジュール",
    chart_subtitle_tasks: "タスク状況",
    chart_tasks_content_title1: "実行中",
    chart_tasks_content_title2: "待機",
    chart_tasks_content_title3: "完了",
    chart_tasks_content_title4: "エラー",
    chart_last_updated_time_title: "最終更新時間",
    schedules_list_no_datas: "データ無し",
    select_box_all: "すべて",

    list_label_group: "グループ",
    list_label_schedule_name: "スケジュール名",
    list_label_start_time: "開始時間",
    list_label_end_time: "終了時間",
    list_label_exit_code: "終了コード",
    list_label_message: "メッセージ",
    list_label_status: "状態",
    list_label_version: "バージョン",
    list_label_actions: "操作",
    list_label_device: "デバイス",
    list_label_last_login_at: "最後ログイン",

    label_active: "アクティブ",
    label_inactive: "非アクティブ",

    btn_filter_reset: "リセット",
    btn_close: "閉じる",
    btn_prev: "戻る",
    btn_next: "次へ",
    btn_edit: "修正",
    btn_remove: "削除",
    btn_add: "追加",
    btn_save: "保存",

    msg_enter_group_name: "グループ名を入力してください。",
    msg_delete_group_name: "グループを削除しますか。",

  },
  ko: {...ko_msg}
}

// Create i18n instance
const i18n = createI18n({
  locale: userLocale,       // Default locale, such as en or the browser language
  fallbackLocale: 'en',     // Fallback locale
  messages                // Pass only the language-specific message objects.
})

export default i18n
