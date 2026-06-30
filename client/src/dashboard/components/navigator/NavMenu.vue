<template>
    <div class="app-header-navigation">
        <nav class="navigation">
            <a href="/dashboard">
                <i class="ph ph-speedometer"></i>
                <span>{{ $t('sub_dashboard') }}</span>
            </a>
            <a href="/dashboard/groups">
                <i class="ph ph-users"></i>
                <span>{{ $t('sub_groups') }}</span>
            </a>
            <a href="/dashboard/devices">
                <i class="ph ph-hard-drives"></i>
                <span>{{ $t('sub_devices') }}</span>
            </a>
            <a href="/dashboard/schedules">
                <i class="ph ph-calendar"></i>
                <span>{{ $t('sub_schedules') }}</span>
            </a>
            <a href="/dashboard/tasks">
                <i class="ph ph-alarm"></i>
                <span>{{ $t('sub_tasks') }}</span>
            </a>
            <a href="/dashboard/schedule-history">
                <i class="ph ph-clock-counter-clockwise"></i>
                <span>{{ $t('sub_history') }}</span>
            </a>
            <a href="/dashboard/users">
                <i class="ph ph-user"></i>
                <span>{{ $t('sub_users') }}</span>
            </a>
            <a href="/dashboard/logout">
              <i class="ph ph-lock-key-open"></i>
                <span>{{ $t('sub_logout') }}</span>
            </a>
        </nav>
    </div>
</template>

