<template>
    <section class="payment-section">
        <h2>New Task</h2>
        <div class="payment-section-header">
            <p>Choose a task type</p>
            <div>
                <button class="card-button active" title="Command">
                    <i class="ph ph-terminal-window"></i>
                </button>
                <button class="card-button" title="Copy">
                    <i class="ph ph-copy"></i>
                </button>
                <button class="card-button" title="Archive">
                    <i class="ph ph-file-archive"></i>
                </button>
            </div>
        </div>
        <div class="device-groups">
            <div class="dropdown-field">
                <i class="ph ph-hard-drives"></i>
                <select>
                    <option>Select Device</option>
                    <option>Home</option>
                    <option>Work</option>
                </select>
            </div>
        </div>
        <div class="faq">
            <p>Enter the run interval</p>
            <div>
                <label>Day of week</label>
                <input type="text" placeholder="*">
            </div>
            <div>
                <label>Day</label>
                <input type="text" placeholder="*">
            </div>
            <div>
                <label>Hour</label>
                <input type="text" placeholder="*">
            </div>
            <div>
                <label>Minute</label>
                <input type="text" placeholder="*">
            </div>
            <div>
                <label>Command</label>
                <input type="text" placeholder="Input command">
            </div>
        </div>
        <div class="payment-section-footer">
            <button class="save-button">
                Save
            </button>
            <button class="save-button">
                Reset
            </button>
        </div>
    </section>
</template>
