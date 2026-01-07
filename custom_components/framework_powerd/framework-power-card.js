class FrameworkPowerCard extends HTMLElement {
  set hass(hass) {
    const entityId = this.config.entity;
    const state = hass.states[entityId];

    if (!state) {
      this.innerHTML = `
        <ha-card header="Framework Power">
          <div class="card-content">
            Entity not found: ${entityId}
          </div>
        </ha-card>
      `;
      return;
    }

    const mode = state.state; // 'performance', 'powersave', 'auto'
    const customName = state.attributes.branding_name || "Framework Power";

    // Simple styling
    const style = `
      <style>
        ha-card {
          padding: 16px;
          display: flex;
          flex-direction: column;
          align-items: center;
        }
        .header {
          display: flex;
          align-items: center;
          font-size: 1.5em;
          font-weight: bold;
          margin-bottom: 8px;
        }
        .header img {
          height: 32px;
          margin-right: 12px;
        }
        .status {
          font-size: 1.1em;
          margin-bottom: 16px;
          color: var(--secondary-text-color);
        }
        .controls {
          display: flex;
          gap: 8px;
          width: 100%;
          justify-content: center;
        }
        button {
          background-color: var(--primary-color);
          color: var(--text-primary-color);
          border: none;
          padding: 10px 16px;
          border-radius: 4px;
          cursor: pointer;
          font-size: 0.9em;
          flex: 1;
          transition: background-color 0.3s;
        }
        button.active {
          background-color: var(--accent-color);
          font-weight: bold;
        }
        button:hover {
          opacity: 0.9;
        }
      </style>
    `;

    // Button logic
    const modes = ['performance', 'auto', 'powersave'];
    const labels = {
      'performance': 'Performance',
      'auto': 'Auto',
      'powersave': 'Powersave'
    };

    this.innerHTML = `
      ${style}
      <ha-card>
        <div class="header">
          <!-- Logo served by integration -->
          <img src="/framework_powerd/logo.png" alt="Logo" onerror="this.style.display='none'">
          <span>${customName}</span>
        </div>
        <div class="status">
          Current Mode: <strong>${mode.charAt(0).toUpperCase() + mode.slice(1)}</strong>
        </div>
        <div class="controls">
          ${modes.map(m => {
      const isActive = mode === m ? 'active' : '';
      return `<button class="${isActive}" data-mode="${m}">${labels[m]}</button>`;
    }).join('')}
        </div>
      </ha-card>
    `;

    // Add event listeners preventing scope issues
    this.querySelectorAll('button').forEach(btn => {
      btn.addEventListener('click', (e) => {
        this.setMode(e.target.dataset.mode);
      });
    });

    // Store hass and entity for method access
    this._hass = hass;
    this._entityId = entityId;
  }

  setMode(mode) {
    if (!this._hass) return;
    this._hass.callService('select', 'select_option', {
      entity_id: this._entityId,
      option: mode
    });
  }

  setConfig(config) {
    if (!config.entity) {
      throw new Error('Please define an entity (e.g. select.framework_power_control)');
    }
    this.config = config;
  }

  getCardSize() {
    return 3;
  }
}

customElements.define('framework-power-card', FrameworkPowerCard);
