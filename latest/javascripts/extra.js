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

});
