// WebSocket connection
let ws = null;
let reconnectInterval = null;
let reconnectAttempts = 0;
const MAX_RECONNECT_INTERVAL = 30000;
let processCounter = 1;
let processList = [];
let isSimulatorInitialized = false;

// DOM Elements
const statusIndicator = document.getElementById('status-indicator');
const statusText = document.getElementById('status-text');
const algorithmSelect = document.getElementById('algorithm');
const timeQuantumInput = document.getElementById('timeQuantum');
const timeQuantumConfig = document.getElementById('timeQuantumConfig');
const speedSlider = document.getElementById('speed');
const speedValue = document.getElementById('speedValue');

// Buttons
const initBtn = document.getElementById('initBtn');
const startBtn = document.getElementById('startBtn');
const pauseBtn = document.getElementById('pauseBtn');
const resumeBtn = document.getElementById('resumeBtn');
const stepBtn = document.getElementById('stepBtn');
const resetBtn = document.getElementById('resetBtn');
const stopBtn = document.getElementById('stopBtn');
const addProcessBtn = document.getElementById('addProcessBtn');
const loadDefaultBtn = document.getElementById('loadDefaultBtn');

// Display elements
const currentTimeSpan = document.getElementById('currentTime');
const currentAlgorithmSpan = document.getElementById('currentAlgorithm');
const simulationStateSpan = document.getElementById('simulationState');

// Initialize WebSocket connection
function connectWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws`;

    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
        console.log('WebSocket connected');
        statusIndicator.className = 'status-connected';
        statusText.textContent = 'Connected';
        reconnectAttempts = 0;
        if (reconnectInterval) {
            clearTimeout(reconnectInterval);
            reconnectInterval = null;
        }
    };

    ws.onmessage = (event) => {
        const data = JSON.parse(event.data);
        handleMessage(data);
    };

    ws.onclose = () => {
        console.log('WebSocket disconnected');
        statusIndicator.className = 'status-disconnected';
        statusText.textContent = 'Disconnected';

        // Exponential backoff up to MAX_RECONNECT_INTERVAL so a down server
        // does not hammer it with 3s retries forever.
        if (reconnectInterval) {
            clearInterval(reconnectInterval);
        }
        reconnectAttempts = 0;
        scheduleReconnect();
    };

    ws.onerror = (error) => {
        console.error('WebSocket error:', error);
    };
}

// Send message to server
function sendMessage(message) {
    if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify(message));
    } else {
        console.error('WebSocket not connected');
    }
}

// scheduleReconnect retries the WebSocket connection with capped exponential
// backoff: 1s, 2s, 4s, 8s, ... up to MAX_RECONNECT_INTERVAL.
function scheduleReconnect() {
    const delay = Math.min(1000 * Math.pow(2, reconnectAttempts), MAX_RECONNECT_INTERVAL);
    reconnectAttempts++;
    console.log(`Reconnecting in ${delay}ms (attempt ${reconnectAttempts})`);
    reconnectInterval = setTimeout(() => {
        reconnectInterval = null;
        connectWebSocket();
    }, delay);
}

// Handle incoming messages
function handleMessage(data) {
    if (data.type === 'success' || data.type === 'error') {
        console.log(data.type + ':', data.message);
        return;
    }

    // Update simulation state
    updateSimulationDisplay(data);
}

// escapeHtml prevents XSS when injecting user-controlled strings (e.g. process
// names) into innerHTML. Non-string / null values are stringified first.
function escapeHtml(value) {
    if (value === null || value === undefined) {
        return '';
    }
    return String(value)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

// num safely formats a possibly-undefined number for display.
function num(value, fallback) {
    return (value === undefined || value === null || isNaN(value)) ? (fallback || 0) : value;
}

// Update all display elements
function updateSimulationDisplay(state) {
    updateCurrentTime(state.currentTime);
    updateCurrentProcess(state.currentProcess);
    updateReadyQueue(state.readyQueue);
    updateCompletedProcesses(state.completedProcesses);
    updateGanttChart(state.ganttChart);
    updateEventLog(state.events);
    updateMetrics(state.metrics);
    updateProcessTable(state);
    updateSimulationState(state.state);
    updateAlgorithmName(state.algorithm);
}

// Update current time
function updateCurrentTime(time) {
    currentTimeSpan.textContent = time || 0;
}

// Update current process display
function updateCurrentProcess(process) {
    const container = document.getElementById('currentProcess');

    if (!process) {
        container.innerHTML = '<div class="no-process">No process running</div>';
        return;
    }

    container.innerHTML = `
        <div class="process-box" style="border-left-color: ${escapeHtml(process.Color)}">
            <div><strong>PID:</strong> ${escapeHtml(process.PID)} (${escapeHtml(process.Name)})</div>
            <div><strong>Remaining Time:</strong> ${num(process.RemainingTime)} / ${num(process.BurstTime)}</div>
            <div><strong>Priority:</strong> ${escapeHtml(process.Priority)}</div>
            <div><strong>State:</strong> <span class="state-${escapeHtml(process.State)}">${getStateName(process.State)}</span></div>
        </div>
    `;
}

// Update ready queue display
function updateReadyQueue(queue) {
    const container = document.getElementById('readyQueue');

    if (!queue || queue.length === 0) {
        container.innerHTML = '<div class="no-process">Empty</div>';
        return;
    }

    container.innerHTML = queue.map(p => `
        <div class="process-box" style="border-left-color: ${escapeHtml(p.Color)}">
            <div><strong>PID:</strong> ${escapeHtml(p.PID)} (${escapeHtml(p.Name)})</div>
            <div><strong>Remaining:</strong> ${num(p.RemainingTime)} / ${num(p.BurstTime)}</div>
            <div><strong>Priority:</strong> ${escapeHtml(p.Priority)}</div>
        </div>
    `).join('');
}

// Update completed processes display
function updateCompletedProcesses(processes) {
    const container = document.getElementById('completedProcesses');

    if (!processes || processes.length === 0) {
        container.innerHTML = '<div class="no-process">None</div>';
        return;
    }

    container.innerHTML = processes.map(p => `
        <div class="process-box" style="border-left-color: ${escapeHtml(p.Color)}">
            <div><strong>PID:</strong> ${escapeHtml(p.PID)} (${escapeHtml(p.Name)})</div>
            <div><strong>Turnaround:</strong> ${num(p.TurnaroundTime)}</div>
            <div><strong>Waiting:</strong> ${num(p.WaitingTime)}</div>
        </div>
    `).join('');
}

// Update Gantt chart
function updateGanttChart(ganttData) {
    const container = document.getElementById('ganttChart');

    if (!ganttData || ganttData.length === 0) {
        container.innerHTML = '<div class="gantt-placeholder">Gantt chart will appear here during simulation</div>';
        return;
    }

    const maxTime = ganttData[ganttData.length - 1].EndTime;
    const pixelsPerUnit = Math.max(20, Math.min(100, Math.floor(800 / Math.max(1, maxTime))));

    let ganttHTML = '<div class="gantt-container">';
    ganttData.forEach(entry => {
        const width = (entry.EndTime - entry.StartTime) * pixelsPerUnit;
        const label = entry.PID === -1 ? 'IDLE' : `P${escapeHtml(entry.PID)}`;

        ganttHTML += `
            <div class="gantt-entry" style="width: ${width}px; background-color: ${escapeHtml(entry.Color)}; color: white;">
                ${label}
            </div>
        `;
    });
    ganttHTML += '</div>';

    // Time markers: render one per unit, labelling every step interval so the
    // axis stays readable for long simulations.
    const step = Math.max(1, Math.ceil(maxTime / 20));
    ganttHTML += '<div class="gantt-time">';
    for (let i = 0; i <= maxTime; i++) {
        const marker = (i % step === 0) ? i : '';
        ganttHTML += `<div class="time-marker" style="width: ${pixelsPerUnit}px;">${marker}</div>`;
    }
    ganttHTML += '</div>';

    container.innerHTML = ganttHTML;
}

// Update event log
function updateEventLog(events) {
    const container = document.getElementById('eventLog');

    if (!events || events.length === 0) {
        container.innerHTML = '<div class="no-events">No events yet</div>';
        return;
    }

    // Show last 20 events
    const recentEvents = events.slice(-20).reverse();

    container.innerHTML = recentEvents.map(event => `
        <div class="event-entry event-${escapeHtml(event.EventType)}">
            <div class="event-time">Time ${num(event.Time)} - ${escapeHtml(event.EventType.toUpperCase())}</div>
            <div class="event-description">${escapeHtml(event.Description)}</div>
        </div>
    `).join('');
}

// Update metrics
function updateMetrics(metrics) {
    if (!metrics) return;

    document.getElementById('avgTurnaround').textContent = num(metrics.AverageTurnaroundTime).toFixed(2);
    document.getElementById('avgWaiting').textContent = num(metrics.AverageWaitingTime).toFixed(2);
    document.getElementById('avgResponse').textContent = num(metrics.AverageResponseTime).toFixed(2);
    document.getElementById('cpuUtil').textContent = num(metrics.CPUUtilization).toFixed(2) + '%';
    document.getElementById('throughput').textContent = num(metrics.Throughput).toFixed(3);
    document.getElementById('contextSwitches').textContent = num(metrics.ContextSwitches);
    document.getElementById('totalProcesses').textContent = num(metrics.TotalProcesses);
    document.getElementById('completedCount').textContent = num(metrics.CompletedProcesses);
}

// Update process table
function updateProcessTable(state) {
    const tbody = document.getElementById('processTableBody');

    // Get all processes (combine ready, running, and completed)
    const allProcesses = [];

    if (state.currentProcess) {
        allProcesses.push(state.currentProcess);
    }

    if (state.readyQueue) {
        allProcesses.push(...state.readyQueue);
    }

    if (state.completedProcesses) {
        allProcesses.push(...state.completedProcesses);
    }

    // Remove duplicates and sort by PID
    const uniqueProcesses = Array.from(new Map(allProcesses.map(p => [p.PID, p])).values())
        .sort((a, b) => a.PID - b.PID);

    if (uniqueProcesses.length === 0) {
        tbody.innerHTML = '<tr><td colspan="11" class="no-data">No processes</td></tr>';
        return;
    }

    tbody.innerHTML = uniqueProcesses.map(p => `
        <tr>
            <td>${escapeHtml(p.PID)}</td>
            <td>${escapeHtml(p.Name)}</td>
            <td>${num(p.ArrivalTime)}</td>
            <td>${num(p.BurstTime)}</td>
            <td>${escapeHtml(p.Priority)}</td>
            <td class="state-${escapeHtml(p.State)}">${getStateName(p.State)}</td>
            <td>${p.StartTime >= 0 ? p.StartTime : '-'}</td>
            <td>${p.CompletionTime > 0 ? p.CompletionTime : '-'}</td>
            <td>${p.TurnaroundTime > 0 ? p.TurnaroundTime : '-'}</td>
            <td>${p.WaitingTime >= 0 ? p.WaitingTime : '-'}</td>
            <td>${p.ResponseTime >= 0 ? p.ResponseTime : '-'}</td>
        </tr>
    `).join('');
}

// Update simulation state
function updateSimulationState(state) {
    simulationStateSpan.textContent = state || 'idle';

    // Update button states
    const isRunning = state === 'running';
    const isPaused = state === 'paused';
    const isComplete = state === 'complete';
    const isIdle = state === 'idle';

    startBtn.disabled = !isSimulatorInitialized || isRunning || isComplete;
    pauseBtn.disabled = !isRunning;
    resumeBtn.disabled = !isPaused;
    stepBtn.disabled = !isSimulatorInitialized || isComplete;
    resetBtn.disabled = !isSimulatorInitialized;
    stopBtn.disabled = isIdle || isComplete;
}

// Update algorithm name
function updateAlgorithmName(algorithm) {
    currentAlgorithmSpan.textContent = algorithm || '-';
}

// Get state name from state code
function getStateName(state) {
    const stateNames = {
        0: 'New',
        1: 'Ready',
        2: 'Running',
        3: 'Waiting',
        4: 'Terminated'
    };
    return stateNames[state] || 'Unknown';
}

// Initialize simulator
function initializeSimulator() {
    const algorithm = algorithmSelect.value;
    const timeQuantum = parseInt(timeQuantumInput.value);

    if (processList.length === 0) {
        alert('Please add at least one process before initializing');
        return;
    }

    if (algorithm === 'rr' && (isNaN(timeQuantum) || timeQuantum < 1)) {
        alert('Time quantum must be a positive integer for Round-Robin');
        return;
    }

    sendMessage({
        type: 'init',
        algorithm: algorithm,
        timeQuantum: isNaN(timeQuantum) ? 4 : timeQuantum,
        processes: processList
    });

    isSimulatorInitialized = true;
    startBtn.disabled = false;
    resetBtn.disabled = false;
    stepBtn.disabled = false;
}

// Add process
function addProcess() {
    const name = document.getElementById('processName').value.trim() || `P${processCounter}`;
    const arrival = parseInt(document.getElementById('arrivalTime').value);
    const burst = parseInt(document.getElementById('burstTime').value);
    const priority = parseInt(document.getElementById('priority').value);

    if (isNaN(arrival) || arrival < 0) {
        alert('Arrival time must be a non-negative integer');
        return;
    }
    if (isNaN(burst) || burst < 0) {
        alert('Burst time must be a non-negative integer');
        return;
    }
    if (isNaN(priority)) {
        alert('Priority must be an integer');
        return;
    }

    const process = {
        pid: processCounter++,
        name: name,
        arrivalTime: arrival,
        burstTime: burst,
        priority: priority
    };

    processList.push(process);
    updateProcessListDisplay();

    // Clear inputs
    document.getElementById('processName').value = '';
    document.getElementById('arrivalTime').value = '0';
    document.getElementById('burstTime').value = '5';
    document.getElementById('priority').value = '0';
}

// Remove a process from the pending list (by index).
function removeProcess(index) {
    processList.splice(index, 1);
    updateProcessListDisplay();
}

// Load default process set
function loadDefaultProcesses() {
    processList = [
        { pid: 1, name: 'P1', arrivalTime: 0, burstTime: 8, priority: 2 },
        { pid: 2, name: 'P2', arrivalTime: 1, burstTime: 4, priority: 1 },
        { pid: 3, name: 'P3', arrivalTime: 2, burstTime: 9, priority: 3 },
        { pid: 4, name: 'P4', arrivalTime: 3, burstTime: 5, priority: 0 },
        { pid: 5, name: 'P5', arrivalTime: 4, burstTime: 2, priority: 1 }
    ];
    processCounter = 6;
    updateProcessListDisplay();
}

// Update process list display
function updateProcessListDisplay() {
    const container = document.getElementById('processList');

    if (processList.length === 0) {
        container.innerHTML = '<div class="no-process">No processes added</div>';
        return;
    }

    container.innerHTML = processList.map((p, index) => `
        <div class="process-chip">
            <span class="process-color" style="background-color: ${escapeHtml(generateColor(p.pid))}"></span>
            <span>${escapeHtml(p.name)} (A:${num(p.arrivalTime)}, B:${num(p.burstTime)}, P:${escapeHtml(p.priority)})</span>
            <button class="chip-remove" title="Remove" onclick="removeProcess(${index})">×</button>
        </div>
    `).join('');
}

// Generate color for process
function generateColor(pid) {
    const colors = [
        '#4A90E2', '#50C878', '#E74C3C', '#F39C12', '#9B59B6',
        '#1ABC9C', '#E67E22', '#3498DB', '#2ECC71', '#E91E63',
        '#00BCD4', '#FF5722', '#795548', '#607D8B', '#CDDC39'
    ];
    return colors[(pid - 1) % colors.length];
}

// Event Listeners
initBtn.addEventListener('click', initializeSimulator);
startBtn.addEventListener('click', () => sendMessage({ type: 'start' }));
pauseBtn.addEventListener('click', () => sendMessage({ type: 'pause' }));
resumeBtn.addEventListener('click', () => sendMessage({ type: 'resume' }));
stepBtn.addEventListener('click', () => sendMessage({ type: 'step' }));
resetBtn.addEventListener('click', () => sendMessage({ type: 'reset' }));
stopBtn.addEventListener('click', () => sendMessage({ type: 'stop' }));
addProcessBtn.addEventListener('click', addProcess);
loadDefaultBtn.addEventListener('click', loadDefaultProcesses);

// Speed slider
speedSlider.addEventListener('input', (e) => {
    const speed = parseInt(e.target.value);
    speedValue.textContent = `${speed} ms`;
    if (isSimulatorInitialized) {
        sendMessage({ type: 'speed', speed: speed });
    }
});

// Algorithm change handler
algorithmSelect.addEventListener('change', (e) => {
    const algo = e.target.value;
    // Show/hide time quantum based on algorithm
    if (algo === 'rr' || algo === 'lottery' || algo === 'mlq') {
        timeQuantumConfig.style.display = 'block';
    } else {
        timeQuantumConfig.style.display = 'none';
    }
});

// Initialize
connectWebSocket();
updateProcessListDisplay();
updateSimulationState('idle'); // set correct initial button disabled states

// Set initial time quantum visibility
if (algorithmSelect.value === 'rr' || algorithmSelect.value === 'lottery' || algorithmSelect.value === 'mlq') {
    timeQuantumConfig.style.display = 'block';
} else {
    timeQuantumConfig.style.display = 'none';
}
