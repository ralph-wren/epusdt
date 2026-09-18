if ('serviceWorker' in navigator) {
  let refreshing = false

  navigator.serviceWorker.addEventListener('controllerchange', () => {
    if (refreshing) return
    refreshing = true
    window.location.reload()
  })

  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register('/sw.js?v=cashier-rate-v3', {
        scope: '/',
        updateViaCache: 'none',
      })
      .then((registration) => registration.update())
      .catch(() => undefined)
  })
}
