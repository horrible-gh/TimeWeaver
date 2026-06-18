// src/i18n.js
import { createI18n } from 'vue-i18n'

const userLocale = navigator.language.split('-')[0] || 'en';

// 각 언어별 메시지 객체만 정의합니다.
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
  ko: {
    sub_dashboard: '대시보드',
    sub_devices: "장치 관리",
    sub_schedules: "스케줄 관리",
    sub_history: "스케줄 이력",
    sub_groups: "그룹 관리",
    sub_users: "사용자 관리",
    sub_logout:"로그아웃",

    chart_title: "서비스",
    chart_subtitle_device: "디바이스 현황",
    chart_devices_content1: "활성",
    chart_devices_content2: "에러",
    chart_devices_content3: "비활성",
    chart_devices_content_title: "장치",
    chart_subtitle_schedules: "스케줄 현황",
    chart_schedules_content1: "활성",
    chart_schedules_content2: "에러",
    chart_schedules_content3: "비활성",
    chart_schedules_content_title: "스케줄",
    chart_subtitle_tasks: "태스크 현황",
    chart_tasks_content_title1: "진행중",
    chart_tasks_content_title2: "대기",
    chart_tasks_content_title3: "완료",
    chart_tasks_content_title4: "에러",
    chart_last_updated_time_title: "최종 갱신 시간",
    schedules_list_no_datas: "데이터 없음",
    select_box_all: "전체",

    list_label_group: "그룹",
    list_label_schedule_name: "스케줄 이름",
    list_label_start_time: "시작 시간",
    list_label_end_time: "종료 시간",
    list_label_exit_code: "종료 코드",
    list_label_message: "종료 메세지",
    list_label_status: "상태",
    list_label_version: "버전",
    list_label_actions: "액션",
    list_label_device: "장치",
    list_label_last_login_at: "마지막 로그인",

    label_active: "활성",
    label_inactive: "비활성",

    btn_filter_reset: "검색 초기화",
    btn_close: "닫기",
    btn_prev: "이전",
    btn_next: "다음",
    btn_edit: "수정",
    btn_remove: "삭제",
    btn_add: "추가",
    btn_save: "저장",

    msg_enter_group_name: "그룹 이름을 입력하세요",
    msg_delete_group_name: "정말 그룹을 삭제하시겠습니까?",
  }
}

// i18n 인스턴스 생성
const i18n = createI18n({
  locale: userLocale,       // 기본 로케일 (예: 'en' 또는 브라우저의 언어)
  fallbackLocale: 'en',     // fallback 로케일
  messages                // 언어별 메시지 객체만 전달합니다.
})

export default i18n
