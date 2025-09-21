/* Custom JavaScript for KrakenHashes documentation */

// Add copy button text feedback
document.addEventListener('DOMContentLoaded', function() {
  // Enhance code copy functionality
  const buttons = document.querySelectorAll('button[data-clipboard-target]');
  buttons.forEach(button => {
    button.addEventListener('click', function() {
      const originalText = button.innerHTML;
      button.innerHTML = 'Copied!';
      button.classList.add('copied');
      
      setTimeout(() => {
        button.innerHTML = originalText;
        button.classList.remove('copied');
      }, 2000);
    });
  });

  // Add anchor links to headers
  const headers = document.querySelectorAll('h2[id], h3[id], h4[id]');
  headers.forEach(header => {
    const anchor = document.createElement('a');
    anchor.className = 'header-anchor';
    anchor.href = '#' + header.id;
    anchor.innerHTML = '🔗';
    anchor.title = 'Link to this section';
    header.appendChild(anchor);
  });

  // Smooth scroll for anchor links
  document.querySelectorAll('a[href^="#"]').forEach(anchor => {
    anchor.addEventListener('click', function(e) {
      e.preventDefault();
      const target = document.querySelector(this.getAttribute('href'));
      if (target) {
        target.scrollIntoView({
          behavior: 'smooth',
          block: 'start'
        });
      }
    });
  });

  // Add external link indicators
  const links = document.querySelectorAll('.md-content a[href^="http"]');
  links.forEach(link => {
    if (!link.hostname || link.hostname !== window.location.hostname) {
      link.classList.add('external-link');
      link.setAttribute('target', '_blank');
      link.setAttribute('rel', 'noopener noreferrer');
    }
  });

  // Version warning for alpha
  if (!localStorage.getItem('kh-alpha-warning-dismissed')) {
    const banner = document.createElement('div');
    banner.className = 'alpha-warning-banner';
    banner.innerHTML = `
      <div class="alpha-warning-content">
        <strong>⚠️ Alpha Software:</strong> KrakenHashes is in active development. 
        Breaking changes are expected. Not for production use.
        <button class="dismiss-alpha-warning" onclick="dismissAlphaWarning()">✕</button>
      </div>
    `;
    document.body.insertBefore(banner, document.body.firstChild);
  }
});

// Dismiss alpha warning
function dismissAlphaWarning() {
  localStorage.setItem('kh-alpha-warning-dismissed', 'true');
  document.querySelector('.alpha-warning-banner').remove();
}

// Add style for alpha warning banner
const style = document.createElement('style');
style.textContent = `
  .alpha-warning-banner {
    background-color: #ff9800;
    color: white;
    padding: 10px;
    text-align: center;
    position: sticky;
    top: 0;
    z-index: 1000;
    box-shadow: 0 2px 4px rgba(0,0,0,0.2);
  }
  
  .alpha-warning-content {
    max-width: 1200px;
    margin: 0 auto;
    position: relative;
    padding: 0 40px;
  }
  
  .dismiss-alpha-warning {
    position: absolute;
    right: 10px;
    top: 50%;
    transform: translateY(-50%);
    background: none;
    border: none;
    color: white;
    font-size: 20px;
    cursor: pointer;
    padding: 5px;
  }
  
  .header-anchor {
    opacity: 0;
    margin-left: 0.5em;
    text-decoration: none;
    font-size: 0.8em;
    transition: opacity 0.2s;
  }
  
  h2:hover .header-anchor,
  h3:hover .header-anchor,
  h4:hover .header-anchor {
    opacity: 1;
  }
  
  .external-link::after {
    content: " ↗";
    font-size: 0.8em;
    opacity: 0.6;
  }
  
  button.copied {
    background-color: #4caf50 !important;
  }
`;
document.head.appendChild(style);