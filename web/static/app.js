(() => {
  function onActivityPage() {
    const mapStyleURL = window.__MAP_STYLE_URL__;
    if (!mapStyleURL) return;
    const m = location.pathname.match(/\/activity\/(\d+)/);
    if (!m) return;
    const id = m[1];
    const map = new maplibregl.Map({
      container: 'map',
      style: mapStyleURL,
      center: [0,0],
      zoom: 2
    });
    installMissingStyleImageFallback(map);
    fetch('/api/activities/' + id + '/points').then(r=>r.json()).then(points => {
      if (!Array.isArray(points) || points.length===0) return;
      const lineCoords = points.map(p => [p.lng, p.lat]);
      const features = points.map((p, idx) => ({
        type: 'Feature',
        geometry: { type: 'Point', coordinates: [p.lng, p.lat] },
        properties: { idx, time: p.time, speed: p.speed, cadence: p.cadence, heartrate: p.heartrate, alt: p.altitude, grade: p.grade, moving: p.moving }
      }));
      const fc = { type: 'FeatureCollection', features };

      map.on('load', async () => {
        const routeFeature = {
          type: 'Feature',
          geometry: { type: 'LineString', coordinates: lineCoords }
        };

        try {
          map.addSource('route-plain', { type: 'geojson', data: routeFeature });
          map.addLayer({
            id: 'route-plain-line',
            type: 'line',
            source: 'route-plain',
            layout: { 'line-cap': 'round', 'line-join': 'round' },
            paint: { 'line-color': '#7cc8ff', 'line-width': 5, 'line-opacity': 0.95 }
          });
          map.addSource('route', { type: 'geojson', lineMetrics: true, data: routeFeature });
          map.addLayer({
            id: 'route-line',
            type: 'line',
            source: 'route',
            layout: { 'line-cap': 'round', 'line-join': 'round' },
            paint: {
              'line-color': '#7cc8ff',
              'line-gradient': solidLineGradient('#7cc8ff'),
              'line-width': 5,
              'line-opacity': 0
            }
          });
        } catch (e) {
          console.warn('Error adding route source/layer:', e);
        }

        // Direction arrows along the route
        // Provide our own small arrow image for direction markers
        try {
          if (!map.hasImage('dir-arrow')) {
            const size = 32;
            const canvas = document.createElement('canvas');
            canvas.width = size; canvas.height = size;
            const ctx = canvas.getContext('2d');
            ctx.clearRect(0,0,size,size);
            // Draw a simple right-pointing triangle
            ctx.fillStyle = '#7cc8ff';
            ctx.beginPath();
            ctx.moveTo(size*0.2, size*0.2);
            ctx.lineTo(size*0.2, size*0.8);
            ctx.lineTo(size*0.85, size*0.5);
            ctx.closePath();
            ctx.fill();
            map.addImage('dir-arrow', ctx.getImageData(0,0,size,size));
          }

          map.addLayer({
            id: 'route-arrows',
            type: 'symbol',
            source: 'route-plain',
            layout: {
              'symbol-placement': 'line',
              'symbol-spacing': 150,
              'icon-image': 'dir-arrow',
              'icon-size': 0.75,
              'icon-allow-overlap': true,
              'icon-ignore-placement': true
            }
          });
        } catch (e) {
          console.warn('Error adding direction arrows:', e);
        }

        // Start/finish and max metric markers
        try {
          await loadRouteMarkerImages(map);
          map.addSource('route-endpoints', {
            type: 'geojson',
            data: {
              type: 'FeatureCollection',
              features: [
                { type: 'Feature', geometry: { type: 'Point', coordinates: lineCoords[0] }, properties: { type: 'start', icon: 'route-marker-start' } },
                { type: 'Feature', geometry: { type: 'Point', coordinates: lineCoords[lineCoords.length-1] }, properties: { type: 'finish', icon: 'route-marker-finish' } }
              ]
            }
          });
          map.addLayer({
            id: 'route-endpoint-markers',
            type: 'symbol',
            source: 'route-endpoints',
            layout: {
              'icon-image': ['get', 'icon'],
              'icon-size': 1,
              'icon-anchor': 'bottom',
              'icon-allow-overlap': true,
              'icon-ignore-placement': true
            }
          });

          const maxMarkerFeatures = buildRouteMaxMarkerFeatures(features);
          if (maxMarkerFeatures.length > 0) {
            map.addSource('route-max-markers', {
              type: 'geojson',
              data: { type: 'FeatureCollection', features: maxMarkerFeatures }
            });
            map.addLayer({
              id: 'route-max-point-outlines',
              type: 'circle',
              source: 'route-max-markers',
              paint: {
                'circle-radius': 3,
                'circle-color': 'rgba(255,122,89,0.1)',
                'circle-stroke-color': '#ff7a59',
                'circle-stroke-width': 2,
                'circle-opacity': 1,
                'circle-stroke-opacity': 0.95
              }
            });
            map.addLayer({
              id: 'route-max-markers',
              type: 'symbol',
              source: 'route-max-markers',
              layout: {
                'icon-image': ['get', 'icon'],
                'icon-size': 0.92,
                'icon-anchor': 'center',
                'icon-offset': [
                  'match',
                  ['get', 'type'],
                  'max-hr', ['literal', [-18, -18]],
                  'max-speed', ['literal', [18, -18]],
                  'max-cadence', ['literal', [0, -34]],
                  ['literal', [0, 0]]
                ],
                'icon-allow-overlap': true,
                'icon-ignore-placement': true
              }
            });
          }
        } catch (e) {
          console.warn('Error adding route markers:', e);
        }

        try {
          map.addSource('route-points', { type: 'geojson', data: fc });
          map.addLayer({ id: 'route-points-layer', type: 'circle', source: 'route-points', paint: { 'circle-radius': 3, 'circle-color': '#f72585', 'circle-opacity': 0 } });
          bringRouteMarkerLayersToFront(map);
        } catch (e) {
          console.warn('Error adding route points:', e);
        }

        const bounds = new maplibregl.LngLatBounds();
        for (const c of lineCoords) bounds.extend(c);
        if (!bounds.isEmpty()) map.fitBounds(bounds, { padding: 40, duration: 0 });

        const popup = new maplibregl.Popup({ closeButton: true, closeOnClick: true, className: 'point-popup' });
        let segmentCreationMode = false; // Declare here so it's accessible to both handlers
        map.on('click', 'route-points-layer', (e) => {
          if (segmentCreationMode) return; // Don't show popup in segment creation mode
          const f = e.features && e.features[0];
          if (!f) return;
          const p = f.properties;
          const html = `Speed: ${fmtSpeed(p.speed)}<br/>Cadence: ${fmtInt(p.cadence)}<br/>HR: ${fmtInt(p.heartrate)}<br/>Alt: ${fmtFloat(p.alt)} m<br/>Grade: ${fmtPct(p.grade)}<br/>${p.moving ? 'In motion' : 'Stopped'}<br/>Time: ${fmtTime(p.time)}`;
          popup.setLngLat(e.lngLat).setHTML(html).addTo(map);
        });
        map.on('mouseenter', 'route-points-layer', () => map.getCanvas().style.cursor = 'pointer');
        map.on('mouseleave', 'route-points-layer', () => map.getCanvas().style.cursor = '');

        const select = document.getElementById('color-metric');
        const legend = document.getElementById('legend');
        if (select) {
          const showPlainRoute = () => {
            if (map.getLayer('route-plain-line')) {
              map.setPaintProperty('route-plain-line', 'line-color', '#7cc8ff');
              map.setPaintProperty('route-plain-line', 'line-opacity', 0.95);
              map.setPaintProperty('route-plain-line', 'line-width', 5);
            }
            if (map.getLayer('route-line')) {
              map.setPaintProperty('route-line', 'line-color', '#7cc8ff');
              map.setPaintProperty('route-line', 'line-gradient', solidLineGradient('#7cc8ff'));
              map.setPaintProperty('route-line', 'line-opacity', 0.95);
              map.setPaintProperty('route-line', 'line-width', 5);
            }
          };
          const showMetricRoute = () => {
            if (map.getLayer('route-plain-line')) {
              map.setPaintProperty('route-plain-line', 'line-opacity', 0);
            }
            if (map.getLayer('route-line')) {
              map.setPaintProperty('route-line', 'line-opacity', 0.95);
              map.setPaintProperty('route-line', 'line-width', 5);
            }
          };
          const applyColor = async () => {
            const metric = select.value;
            try {
              if (metric === 'none') {
                // Show points if in segment creation mode, otherwise hide them
                const opacity = segmentCreationMode ? 1 : 0;
                map.setPaintProperty('route-points-layer', 'circle-opacity', opacity);
                map.setPaintProperty('route-points-layer', 'circle-color', '#f72585');
                map.setPaintProperty('route-points-layer', 'circle-radius', 3);
                showPlainRoute();
                if (legend) legend.style.display = 'none';
                return;
              }
              showMetricRoute();
              if (metric === 'moving') {
                map.setPaintProperty('route-points-layer', 'circle-opacity', [
                  'case',
                  ['==', ['get', 'moving'], false],
                  1,
                  0
                ]);
                map.setPaintProperty('route-points-layer', 'circle-color', '#e74c3c');
                map.setPaintProperty('route-line', 'line-gradient', movingLineGradient(features));
                if (legend) legend.style.display = 'none';
              } else if (metric === 'hrzones') {
              try {
                const zr = await fetch('/api/hrzones');
                if (!zr.ok) throw new Error('zones fetch failed');
                const z = await zr.json();
                const colors = ['#1b3a8a', '#00c2ff', '#2ecc71', '#f1c40f', '#e74c3c'];
                const zonesArr = (z && z.heart_rate && Array.isArray(z.heart_rate.zones)) ? z.heart_rate.zones : [];
                if (zonesArr.length === 0) {
                  console.warn('No HR zones available; falling back to HR gradient');
                  const {min, max} = computeRange(features, 'heartrate');
                  map.setPaintProperty('route-points-layer', 'circle-opacity', 1);
                  map.setPaintProperty('route-points-layer', 'circle-color', gradientExpression('heartrate', min, max));
                  map.setPaintProperty('route-line', 'line-gradient', lineProgressGradientExpression(features, 'heartrate', min, max));
                  if (legend) renderGradientLegendVertical(legend, 'HR', min, max);
                  return;
                }
                const zoneSteps = hrZonesExpression({heart_rate:{zones:zonesArr}}, colors);
                map.setPaintProperty('route-points-layer', 'circle-opacity', 1);
                map.setPaintProperty('route-points-layer', 'circle-color', zoneSteps);
                map.setPaintProperty('route-line', 'line-gradient', hrZonesLineGradient(features, zonesArr, colors));
                if (legend) renderZonesLegendVertical(legend, colors, zonesArr);
              } catch (e) {
                console.error('HR zones error', e);
              }
              } else {
                map.setPaintProperty('route-points-layer', 'circle-opacity', 1);
                const {min, max} = computeRange(features, metric);
                const gradExpr = gradientExpression(metric, min, max);
                map.setPaintProperty('route-points-layer', 'circle-color', gradExpr);
                map.setPaintProperty('route-line', 'line-gradient', lineProgressGradientExpression(features, metric, min, max));
                if (legend) renderGradientLegendVertical(legend, labelFor(metric), min, max);
              }
            } catch (e) {
              console.warn('Error applying color metric:', e);
            }
            };
          // Store reference to applyColor for segment creation mode
          select._applyColor = applyColor;
          select.addEventListener('change', applyColor);
          applyColor();
        }

        // Segment creation functionality
        let selectedPoints = [];
        const createSegmentBtn = document.getElementById('create-segment-btn');
        const segmentCreatePanel = document.getElementById('segment-create-panel');
        const segmentStepTitle = document.getElementById('segment-step-title');
        const segmentStepCopy = document.getElementById('segment-step-copy');
        const segmentSummary = document.getElementById('segment-summary');
        const segmentResetBtn = document.getElementById('segment-reset-btn');
        const segmentExitBtn = document.getElementById('segment-exit-btn');
        const segmentSavePanelBtn = document.getElementById('segment-save-panel-btn');
        const segmentModal = document.getElementById('segment-modal');
        const segmentForm = document.getElementById('segment-form');
        const segmentCancelBtn = document.getElementById('segment-cancel-btn');
        const segmentSelectionInfo = document.getElementById('segment-selection-info');

        if (createSegmentBtn && segmentModal && segmentForm && segmentCreatePanel) {
          const removeLayerAndSource = (layerId, sourceId = layerId) => {
            if (map.getLayer(layerId)) map.removeLayer(layerId);
            if (map.getSource(sourceId)) map.removeSource(sourceId);
          };

          const distanceMeters = (fromIdx, toIdx) => {
            let total = 0;
            const start = Math.max(0, Math.min(fromIdx, toIdx));
            const end = Math.min(lineCoords.length - 1, Math.max(fromIdx, toIdx));
            const radius = 6371000;
            for (let i = start + 1; i <= end; i++) {
              const prev = lineCoords[i - 1];
              const curr = lineCoords[i];
              const dLat = (curr[1] - prev[1]) * Math.PI / 180;
              const dLng = (curr[0] - prev[0]) * Math.PI / 180;
              const a = Math.sin(dLat / 2) * Math.sin(dLat / 2) +
                Math.cos(prev[1] * Math.PI / 180) * Math.cos(curr[1] * Math.PI / 180) *
                Math.sin(dLng / 2) * Math.sin(dLng / 2);
              total += radius * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
            }
            return total;
          };

          const formatSegmentDistance = meters => meters >= 1000 ? `${(meters / 1000).toFixed(2)} km` : `${Math.round(meters)} m`;
          const formatSegmentDuration = (fromIdx, toIdx) => {
            const start = points[Math.min(fromIdx, toIdx)];
            const end = points[Math.max(fromIdx, toIdx)];
            if (!start?.time || !end?.time) return 'n/a';
            const seconds = Math.max(0, Math.round((new Date(end.time) - new Date(start.time)) / 1000));
            const h = Math.floor(seconds / 3600);
            const m = Math.floor((seconds % 3600) / 60);
            const s = seconds % 60;
            if (h > 0) return `${h}h ${String(m).padStart(2, '0')}m`;
            return `${m}m ${String(s).padStart(2, '0')}s`;
          };

          const setPanelState = (title, copy) => {
            if (segmentStepTitle) segmentStepTitle.textContent = title;
            if (segmentStepCopy) segmentStepCopy.textContent = copy;
          };

          const setSummary = () => {
            const hasSelection = selectedPoints.length === 2;
            if (!segmentSummary) return;
            segmentSummary.hidden = !hasSelection;
            if (segmentSavePanelBtn) segmentSavePanelBtn.hidden = !hasSelection;
            if (!hasSelection) {
              segmentSummary.innerHTML = '';
              return;
            }
            const [startIdx, endIdx] = selectedPoints;
            segmentSummary.innerHTML = `
              <div class="segment-summary-item">
                <span class="segment-summary-label">Distance</span>
                <span class="segment-summary-value">${formatSegmentDistance(distanceMeters(startIdx, endIdx))}</span>
              </div>
              <div class="segment-summary-item">
                <span class="segment-summary-label">Duration</span>
                <span class="segment-summary-value">${formatSegmentDuration(startIdx, endIdx)}</span>
              </div>
              <div class="segment-summary-item">
                <span class="segment-summary-label">Points</span>
                <span class="segment-summary-value">${endIdx - startIdx + 1}</span>
              </div>
            `;
          };

          const showPointSelection = () => {
            const currentMetric = select ? select.value : 'none';
            if (currentMetric === 'none' && map.getLayer('route-points-layer')) {
              map.setPaintProperty('route-points-layer', 'circle-opacity', 1);
              map.setPaintProperty('route-points-layer', 'circle-color', '#f72585');
              map.setPaintProperty('route-points-layer', 'circle-radius', 4);
            }
          };

          const clearSelectionLayers = () => {
            removeLayerAndSource('segment-preview');
            removeLayerAndSource('segment-first-point');
          };

          const resetSegmentSelection = () => {
            selectedPoints = [];
            clearSelectionLayers();
            setPanelState('Select start', 'Tap the route where the segment should begin.');
            if (segmentSelectionInfo) segmentSelectionInfo.textContent = 'Select a start point, then a finish point on the map.';
            setSummary();
            showPointSelection();
          };

          const openSegmentModal = () => {
            if (selectedPoints.length !== 2) return;
            if (segmentSelectionInfo) {
              segmentSelectionInfo.textContent = `Selected ${formatSegmentDistance(distanceMeters(selectedPoints[0], selectedPoints[1]))} over ${formatSegmentDuration(selectedPoints[0], selectedPoints[1])}. Add a name to save it.`;
            }
            segmentModal.style.display = 'flex';
            const nameInput = document.getElementById('segment-name');
            if (nameInput) setTimeout(() => nameInput.focus(), 0);
          };

          const setSegmentMode = active => {
            segmentCreationMode = active;
            document.body.classList.toggle('segment-mode-active', active);
            createSegmentBtn.textContent = active ? 'Cancel Segment' : 'Create Segment';
            createSegmentBtn.classList.toggle('danger-btn', active);
            segmentCreatePanel.hidden = !active;
            segmentModal.style.display = 'none';

            if (active) {
              resetSegmentSelection();
              map.getCanvas().style.cursor = 'crosshair';
              setTimeout(() => {
                segmentCreatePanel.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
                map.resize();
              }, 0);
            } else {
              resetSegmentSelection();
              map.getCanvas().style.cursor = '';
              if (select?._applyColor) select._applyColor();
              setTimeout(() => map.resize(), 0);
            }
          };

          createSegmentBtn.addEventListener('click', () => setSegmentMode(!segmentCreationMode));

          // Handle point selection
          map.on('click', 'route-points-layer', (e) => {
            if (!segmentCreationMode) return;
            const f = e.features && e.features[0];
            if (!f) return;
            const idx = f.properties.idx;

            if (selectedPoints.length === 2) {
              resetSegmentSelection();
            }
            
            if (selectedPoints.length === 0) {
              selectedPoints = [idx];
              setPanelState('Select finish', 'Tap the route where this segment should end.');
              if (segmentSelectionInfo) segmentSelectionInfo.textContent = `Start selected at point ${idx}. Now select a finish point.`;
              
              // Highlight the first selected point
              const firstPoint = features[idx];
              if (firstPoint) {
                // Remove existing highlight if any
                removeLayerAndSource('segment-first-point');
                
                // Add highlight for first point
                map.addSource('segment-first-point', {
                  type: 'geojson',
                  data: {
                    type: 'FeatureCollection',
                    features: [firstPoint]
                  }
                });
                map.addLayer({
                  id: 'segment-first-point',
                  type: 'circle',
                  source: 'segment-first-point',
                  paint: {
                    'circle-radius': 8,
                    'circle-color': '#f1c40f',
                    'circle-stroke-width': 2,
                    'circle-stroke-color': '#ffffff',
                    'circle-opacity': 1
                  }
                });
              }
            } else if (selectedPoints.length === 1) {
              const startIdx = selectedPoints[0];
              const endIdx = idx;
              if (startIdx === endIdx) {
                setPanelState('Select finish', 'Choose a different point for the segment finish.');
                if (segmentSelectionInfo) segmentSelectionInfo.textContent = 'Please select a different point.';
                return;
              }
              // Ensure start < end
              selectedPoints = [Math.min(startIdx, endIdx), Math.max(startIdx, endIdx)];
              setPanelState('Ready to save', 'Review the selected route section, then name the segment.');
              if (segmentSelectionInfo) segmentSelectionInfo.textContent = `Segment selected: points ${selectedPoints[0]} to ${selectedPoints[1]}.`;

              // Show preview line
              const previewCoords = lineCoords.slice(selectedPoints[0], selectedPoints[1] + 1);
              if (map.getSource('segment-preview')) {
                map.getSource('segment-preview').setData({
                  type: 'Feature',
                  geometry: { type: 'LineString', coordinates: previewCoords }
                });
              } else {
                map.addSource('segment-preview', {
                  type: 'geojson',
                  data: { type: 'Feature', geometry: { type: 'LineString', coordinates: previewCoords } }
                });
                map.addLayer({
                  id: 'segment-preview',
                  type: 'line',
                  source: 'segment-preview',
                  paint: { 'line-color': '#f1c40f', 'line-width': 6, 'line-opacity': 0.9 }
                });
              }
              setSummary();
            }
          });

          if (segmentResetBtn) segmentResetBtn.addEventListener('click', resetSegmentSelection);
          if (segmentExitBtn) segmentExitBtn.addEventListener('click', () => setSegmentMode(false));
          if (segmentSavePanelBtn) segmentSavePanelBtn.addEventListener('click', openSegmentModal);

          // Cancel segment creation
          if (segmentCancelBtn) {
            segmentCancelBtn.addEventListener('click', () => {
              segmentModal.style.display = 'none';
            });
          }

          // Submit segment form
          if (segmentForm) {
            segmentForm.addEventListener('submit', async (e) => {
              e.preventDefault();
              if (selectedPoints.length !== 2) {
                alert('Please select two points on the map.');
                return;
              }

              const name = document.getElementById('segment-name').value.trim();
              if (!name) {
                alert('Please enter a segment name.');
                return;
              }

              const description = document.getElementById('segment-description').value.trim();

              try {
                const response = await fetch('/api/segments', {
                  method: 'POST',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify({
                    name,
                    description,
                    activity_id: parseInt(id),
                    start_index: selectedPoints[0],
                    end_index: selectedPoints[1] + 1 // end_index is exclusive
                  })
                });

                if (!response.ok) {
                  const error = await response.text();
                  throw new Error(error || 'Failed to create segment');
                }

                const segment = await response.json();
                segmentForm.reset();
                const segmentID = segment.id || segment.ID;
                if (segmentID) {
                  window.location.href = `/segment/${segmentID}`;
                } else {
                  setSegmentMode(false);
                  window.location.href = '/segments';
                }
              } catch (error) {
                alert('Error creating segment: ' + error.message);
              }
            });
          }
        }

        // Graph rendering functionality
        let chartInstance = null;
        let graphPoints = null; // Store points for map-graph sync
        const metric1Select = document.getElementById('metric1-select');
        const metric2Select = document.getElementById('metric2-select');
        const xAxisSelect = document.getElementById('xaxis-select');
        const graphCanvas = document.getElementById('graph-canvas');
        const graphContainer = document.getElementById('graph-container');
        
        // Helper function to calculate cumulative distance from points
        const calculateCumulativeDistance = (points) => {
          if (!points || points.length < 2) return [];
          
          const distances = [0]; // First point has 0 distance
          const R = 6371000; // Earth radius in meters
          
          for (let i = 1; i < points.length; i++) {
            const prev = points[i - 1];
            const curr = points[i];
            
            if (!prev.lat || !prev.lng || !curr.lat || !curr.lng) {
              distances.push(distances[i - 1]); // Use previous distance if coordinates missing
              continue;
            }
            
            const dLat = (curr.lat - prev.lat) * Math.PI / 180;
            const dLng = (curr.lng - prev.lng) * Math.PI / 180;
            const a = Math.sin(dLat/2) * Math.sin(dLat/2) +
                       Math.cos(prev.lat * Math.PI / 180) * Math.cos(curr.lat * Math.PI / 180) *
                       Math.sin(dLng/2) * Math.sin(dLng/2);
            const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1-a));
            const distance = R * c;
            
            distances.push(distances[i - 1] + distance);
          }
          
          return distances; // Returns distances in meters
        };

        if (metric1Select && metric2Select && graphCanvas) {
          const updateGraph = async () => {
            const metric1 = metric1Select.value;
            const metric2 = metric2Select.value;
            const xAxisType = xAxisSelect ? xAxisSelect.value : 'time';
            
            const placeholder = document.getElementById('graph-placeholder');
            
            if (!metric1 && !metric2) {
              if (chartInstance) {
                chartInstance.destroy();
                chartInstance = null;
              }
              if (graphContainer) graphContainer.classList.add('graph-empty');
              if (graphCanvas) graphCanvas.style.display = 'none';
              if (placeholder) placeholder.style.display = 'block';
              // Keep container visible so users can select metrics
              return;
            }
            
            // Hide placeholder and show canvas
            if (graphContainer) graphContainer.classList.remove('graph-empty');
            if (placeholder) placeholder.style.display = 'none';
            if (graphCanvas) {
              graphCanvas.style.display = 'block';
              graphCanvas.style.width = '100%';
              graphCanvas.style.height = '250px';
            }
            
            const metrics = [];
            if (metric1) metrics.push(metric1);
            if (metric2) metrics.push(metric2);
            
            const includeZones = metric1 === 'heartrate' || metric2 === 'heartrate';
            const url = `/api/activities/${id}/graph?metrics=${metrics.join(',')}&include_zones=${includeZones}`;
            
            try {
              const response = await fetch(url);
              if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`Failed to fetch graph data: ${response.status} ${errorText}`);
              }
              const data = await response.json();
              
              // Store points for synchronization
              graphPoints = points;
              
              // Prepare datasets
              const datasets = [];
              
              // Get colors from CSS variables
              const getCSSVar = (name) => {
                return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
              };
              
              const colors = {
                speed: getCSSVar('--graph-speed'),
                heartrate: getCSSVar('--graph-heartrate'),
                height: getCSSVar('--graph-height'),
                cadence: getCSSVar('--graph-cadence')
              };
              
              let yAxisIDLeft = 'y';
              let yAxisIDRight = 'y1';
              
              // Helper function to create HR dataset with zone coloring
              // Uses a single dataset with segment coloring to avoid multiple legend entries
              const createHRDataset = (metricData, yAxisID, label) => {
                const zoneColors = [
                  getCSSVar('--graph-hr-zone1'),
                  getCSSVar('--graph-hr-zone2'),
                  getCSSVar('--graph-hr-zone3'),
                  getCSSVar('--graph-hr-zone4'),
                  getCSSVar('--graph-hr-zone5')
                ];
                
                // Create data points with zone information
                const dataPoints = metricData.map((p, idx) => {
                  let xValue;
                  if (xAxisType === 'distance' && p.distance != null) {
                    xValue = p.distance / 1000; // Convert from meters to km
                  } else {
                    xValue = new Date(p.time).getTime();
                  }
                  return {
                    x: xValue,
                    y: p.value,
                    zone: p.zone || null,
                    time: p.time // Keep time for synchronization
                  };
                });
                
                // Create a single dataset
                const dataset = {
                  label: label,
                  data: dataPoints,
                  backgroundColor: 'transparent',
                  yAxisID: yAxisID,
                  borderWidth: 2,
                  pointRadius: 0,
                  pointHoverRadius: 4
                };
                
                // If zones are available, use segment coloring
                if (includeZones && metricData.some(p => p.zone)) {
                  dataset.segment = {
                    borderColor: (ctx) => {
                      // Color the segment based on the zone of the first point (p0)
                      if (!ctx || !ctx.p0 || !ctx.p0.raw) {
                        return colors.heartrate;
                      }
                      const point = ctx.p0.raw;
                      if (point && point.zone && point.zone >= 1 && point.zone <= 5) {
                        return zoneColors[point.zone - 1];
                      }
                      return colors.heartrate;
                    }
                  };
                  // Set default border color (will be overridden by segment coloring)
                  dataset.borderColor = colors.heartrate;
                } else {
                  // No zones, use simple coloring
                  dataset.borderColor = colors.heartrate;
                }
                
                return [dataset];
              };
              
              if (metric1 && data[metric1]) {
                const metricData = data[metric1];
                if (metric1 === 'heartrate') {
                  datasets.push(...createHRDataset(metricData, yAxisIDLeft, 'HR'));
                } else {
                  // Convert speed from m/s to km/h (multiply by 3.6)
                  const convertValue = (value, metric) => {
                    return metric === 'speed' ? value * 3.6 : value;
                  };
                  datasets.push({
                    label: metric1.charAt(0).toUpperCase() + metric1.slice(1),
                    data: metricData.map((p, idx) => {
                      let xValue;
                      if (xAxisType === 'distance' && p.distance != null) {
                        xValue = p.distance / 1000; // Convert from meters to km
                      } else {
                        xValue = new Date(p.time).getTime();
                      }
                      return { 
                        x: xValue, 
                        y: convertValue(p.value, metric1),
                        time: p.time // Keep time for synchronization
                      };
                    }),
                    borderColor: colors[metric1] || getCSSVar('--graph-speed'),
                    backgroundColor: 'transparent',
                    yAxisID: yAxisIDLeft,
                    borderWidth: 2,
                    pointRadius: 0,
                    pointHoverRadius: 4
                  });
                }
              }
              
              if (metric2 && data[metric2]) {
                const metricData = data[metric2];
                if (metric2 === 'heartrate') {
                  datasets.push(...createHRDataset(metricData, metric1 ? yAxisIDRight : yAxisIDLeft, 'HR'));
                } else {
                  // Convert speed from m/s to km/h (multiply by 3.6)
                  const convertValue = (value, metric) => {
                    return metric === 'speed' ? value * 3.6 : value;
                  };
                  datasets.push({
                    label: metric2.charAt(0).toUpperCase() + metric2.slice(1),
                    data: metricData.map((p, idx) => {
                      let xValue;
                      if (xAxisType === 'distance' && p.distance != null) {
                        xValue = p.distance / 1000; // Convert from meters to km
                      } else {
                        xValue = new Date(p.time).getTime();
                      }
                      return { 
                        x: xValue, 
                        y: convertValue(p.value, metric2),
                        time: p.time // Keep time for synchronization
                      };
                    }),
                    borderColor: colors[metric2] || getCSSVar('--graph-cadence'),
                    backgroundColor: 'transparent',
                    yAxisID: metric1 ? yAxisIDRight : yAxisIDLeft,
                    borderWidth: 2,
                    pointRadius: 0,
                    pointHoverRadius: 4
                  });
                }
              }
              
              if (datasets.length === 0) return;
              
              // Destroy existing chart properly
              if (chartInstance) {
                try {
                  chartInstance.destroy();
                } catch (e) {
                  console.warn('Error destroying chart:', e);
                }
                chartInstance = null;
              }
              
              // Create new chart (use setTimeout to ensure canvas is released)
              setTimeout(() => {
                try {
                  const ctx = graphCanvas.getContext('2d');
                  if (!ctx) {
                    console.error('Failed to get canvas context');
                    return;
                  }
                  
                  // Check if Chart is available
                  if (typeof Chart === 'undefined') {
                    console.error('Chart.js is not loaded');
                    return;
                  }
                  
                  chartInstance = new Chart(ctx, {
                    type: 'line',
                    data: { datasets },
                    options: {
                      responsive: true,
                      maintainAspectRatio: false,
                      interaction: {
                        intersect: false,
                        mode: 'index'
                      },
                      plugins: {
                        legend: {
                          display: true,
                          position: 'top',
                          labels: {
                            color: '#e0e0e0'
                          }
                        },
                        tooltip: {
                          callbacks: {
                            title: (context) => {
                              if (xAxisType === 'distance') {
                                const xValue = context[0].parsed.x;
                                return `Distance: ${xValue.toFixed(2)} km`;
                              } else {
                                // Time axis - use default time formatting
                                return context[0].label;
                              }
                            },
                            label: (context) => {
                              const label = context.dataset.label || '';
                              const value = context.parsed.y;
                              let unit = '';
                              if (label === 'Speed') unit = ' km/h';
                              else if (label === 'HR') unit = ' bpm';
                              else if (label === 'Height') unit = ' m';
                              else if (label === 'Cadence') unit = ' rpm';
                              return `${label}: ${value.toFixed(1)}${unit}`;
                            }
                          }
                        }
                      },
                      scales: {
                        x: xAxisType === 'distance' ? {
                          type: 'linear',
                          position: 'bottom',
                          title: {
                            display: true,
                            text: 'Distance (km)',
                            color: '#e0e0e0'
                          },
                          ticks: {
                            color: '#e0e0e0',
                            callback: function(value) {
                              return value.toFixed(1) + ' km';
                            }
                          },
                          grid: {
                            color: '#333'
                          }
                        } : {
                          type: 'time',
                          time: {
                            displayFormats: {
                              minute: 'HH:mm'
                            }
                          },
                          ticks: {
                            color: '#e0e0e0'
                          },
                          grid: {
                            color: '#333'
                          }
                        },
                        y: {
                          position: 'left',
                          ticks: {
                            color: '#e0e0e0'
                          },
                          grid: {
                            color: '#333'
                          }
                        },
                        y1: {
                          type: 'linear',
                          display: metric1 && metric2,
                          position: 'right',
                          ticks: {
                            color: '#e0e0e0'
                          },
                          grid: {
                            drawOnChartArea: false
                          }
                        }
                      },
                      onHover: (event, activeElements) => {
                        if (activeElements.length > 0 && graphPoints) {
                          const element = activeElements[0];
                          const dataIndex = element.index;
                          const datasetIndex = element.datasetIndex;
                          const dataset = chartInstance.data.datasets[datasetIndex];
                          const point = dataset.data[dataIndex];
                          
                          // Find corresponding point on map by time
                          const pointTime = new Date(point.x).getTime();
                          const mapPoint = graphPoints.find(p => {
                            const pTime = new Date(p.time).getTime();
                            return Math.abs(pTime - pointTime) < 1000; // Within 1 second
                          });
                          
                          if (mapPoint && map.getLayer('route-points-layer')) {
                            // Highlight point on map (temporary)
                            // Could add a marker or change point color
                          }
                        }
                      }
                    }
                  });
                  
                  // Map-graph synchronization: when clicking on map, highlight on graph
                  map.on('click', 'route-points-layer', (e) => {
                    if (segmentCreationMode || !chartInstance) return;
                    const f = e.features && e.features[0];
                    if (!f) return;
                    const pointTime = new Date(f.properties.time).getTime();
                    
                    // Find closest point in graph
                    let closestDataset = null;
                    let closestIndex = -1;
                    let minDiff = Infinity;
                    
                    chartInstance.data.datasets.forEach((dataset, dsIdx) => {
                      dataset.data.forEach((dp, idx) => {
                        let diff;
                        if (xAxisType === 'distance') {
                          // For distance axis, compare by time stored in data point
                          if (dp.time) {
                            const dpTime = new Date(dp.time).getTime();
                            diff = Math.abs(dpTime - pointTime);
                          } else {
                            diff = Infinity;
                          }
                        } else {
                          // For time axis, compare x values directly
                          diff = Math.abs(dp.x - pointTime);
                        }
                        if (diff < minDiff) {
                          minDiff = diff;
                          closestDataset = dsIdx;
                          closestIndex = idx;
                        }
                      });
                    });
                    
                    if (closestDataset !== null && closestIndex >= 0) {
                      chartInstance.setActiveElements([{ datasetIndex: closestDataset, index: closestIndex }]);
                      chartInstance.update('none');
                    }
                  });
                } catch (error) {
                  console.error('Error creating chart:', error);
                }
              }, 10);
            } catch (error) {
              console.error('Error loading graph:', error);
            }
          };
          
          metric1Select.addEventListener('change', updateGraph);
          metric2Select.addEventListener('change', updateGraph);
          if (xAxisSelect) {
            xAxisSelect.addEventListener('change', updateGraph);
          }
          updateGraph();
        }
      });
    });
  }

  function installMissingStyleImageFallback(map) {
    map.on('styleimagemissing', event => {
      const id = event && event.id;
      if (!id || map.hasImage(id)) return;
      const size = Math.max(8, Math.min(64, Number((id.match(/-(\d+)$/) || [])[1]) || 16));
      const canvas = document.createElement('canvas');
      canvas.width = size;
      canvas.height = size;
      const ctx = canvas.getContext('2d');
      ctx.clearRect(0, 0, size, size);
      map.addImage(id, ctx.getImageData(0, 0, size, size));
    });
  }

  function renderGradientLegendVertical(el, label, min, max) {
    const mid = (min + max) / 2;
    el.classList.add('vertical');
    el.style.display = 'grid';
    el.innerHTML = `
      <div class="legend-vbar">
        <span class="legend-vdot" style="top:0;background:#e74c3c"></span>
        <span class="legend-vdot" style="top:calc(50% - 5px);background:#f1c40f"></span>
        <span class="legend-vdot" style="bottom:0;background:#2ecc71"></span>
      </div>
      <div class="legend-vlabels">
        <div class="legend-vrow"><span class="legend-label">${fmtLegend(label)} max: ${fmtValue(label,max)}</span></div>
        <div class="legend-vrow"><span class="legend-label">mid: ${fmtValue(label,mid)}</span></div>
        <div class="legend-vrow"><span class="legend-label">min: ${fmtValue(label,min)}</span></div>
      </div>`;
  }

  function renderZonesLegendVertical(el, colors, zones) {
    el.classList.add('vertical');
    el.style.display = 'grid';
    const zoneText = (z) => {
      const hasMin = typeof z.min === 'number';
      const hasMax = typeof z.max === 'number';
      // Strava may return max as 0 or -1 for open-ended last zone; treat as infinity
      const openEnded = !hasMax || (typeof z.max === 'number' && z.max <= 0);
      if (hasMin && !openEnded && hasMax) return `${z.min}–${z.max} bpm`;
      if (hasMin) return `≥ ${z.min} bpm`;
      if (hasMax) return `≤ ${z.max} bpm`;
      return '';
    };
    const rows = [];
    const count = Math.min(colors.length, Array.isArray(zones)? zones.length : 0);
    for (let i=0;i<count;i++) {
      rows.push(`<div class="legend-vrow"><span class="legend-swatch" style="background:${colors[i]}"></span><span class="legend-label">Z${i+1} ${zoneText(zones[i])}</span></div>`);
    }
    // For HR zones we do not show a standard ruler/bar, only colored points with labels
    el.innerHTML = `
      <div></div>
      <div class="legend-vlabels">${rows.join('')}</div>`;
  }

  async function loadRouteMarkerImages(map) {
    const markerIcons = [
      { id: 'route-marker-start', path: '/static/icons/point.svg', color: '#47d18c', size: 40 },
      { id: 'route-marker-finish', path: '/static/icons/point.svg', color: '#e74c3c', size: 40 },
      { id: 'route-marker-hr', path: '/static/icons/hr.svg', color: '#ff7a59', size: 34 },
      { id: 'route-marker-speed', path: '/static/icons/speed.svg', color: '#ff7a59', size: 34 },
      { id: 'route-marker-cadence', path: '/static/icons/cadence.svg', color: '#ff7a59', size: 34 }
    ];

    await Promise.all(markerIcons.map(icon => addTintedSvgImage(map, icon)));
  }

  async function addTintedSvgImage(map, icon) {
    if (map.hasImage(icon.id)) return;

    const response = await fetch(icon.path);
    if (!response.ok) throw new Error(`Failed to load ${icon.path}`);
    const svg = await response.text();
    const imageData = await svgToImageData(tintSvg(svg, icon.color), icon.size);
    map.addImage(icon.id, imageData);
  }

  function tintSvg(svg, color) {
    return svg
      .replace(/stroke="#000000"/g, `stroke="${color}"`)
      .replace(/stroke="black"/g, `stroke="${color}"`)
      .replace(/fill="#000000"/g, `fill="${color}"`)
      .replace(/fill="black"/g, `fill="${color}"`);
  }

  function svgToImageData(svg, size) {
    return new Promise((resolve, reject) => {
      const image = new Image();
      const canvas = document.createElement('canvas');
      canvas.width = size;
      canvas.height = size;
      const ctx = canvas.getContext('2d');

      image.onload = () => {
        ctx.clearRect(0, 0, size, size);
        ctx.shadowColor = 'rgba(14,17,22,0.95)';
        ctx.shadowBlur = 3;
        ctx.shadowOffsetX = 0;
        ctx.shadowOffsetY = 1;
        ctx.drawImage(image, 0, 0, size, size);
        ctx.shadowColor = 'transparent';
        ctx.shadowBlur = 0;
        ctx.drawImage(image, 0, 0, size, size);
        resolve(ctx.getImageData(0, 0, size, size));
      };
      image.onerror = reject;
      image.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
    });
  }

  function buildRouteMaxMarkerFeatures(features) {
    return [
      ...maxMetricMarkerFeatures(features, {
        metric: 'heartrate',
        type: 'max-hr',
        icon: 'route-marker-hr'
      }),
      ...maxMetricMarkerFeatures(features, {
        metric: 'speed',
        type: 'max-speed',
        icon: 'route-marker-speed'
      }),
      ...maxMetricMarkerFeatures(features, {
        metric: 'cadence',
        type: 'max-cadence',
        icon: 'route-marker-cadence'
      })
    ];
  }

  function bringRouteMarkerLayersToFront(map) {
    [
      'route-max-point-outlines',
      'route-endpoint-markers',
      'route-max-markers'
    ].forEach(layerId => {
      if (map.getLayer(layerId)) map.moveLayer(layerId);
    });
  }

  function maxMetricMarkerFeatures(features, config) {
    const values = features.map(feature => Number(feature.properties?.[config.metric]));
    const maxValue = values.reduce((max, value) => {
      if (!Number.isFinite(value) || value <= 0) return max;
      return Math.max(max, value);
    }, -Infinity);
    if (!Number.isFinite(maxValue)) return [];

    const maxIndices = [];
    values.forEach((value, idx) => {
      if (Number.isFinite(value) && value === maxValue) maxIndices.push(idx);
    });

    const markerIndices = [];
    for (let i = 0; i < maxIndices.length;) {
      const start = maxIndices[i];
      let end = start;
      while (i + 1 < maxIndices.length && maxIndices[i + 1] === end + 1) {
        i++;
        end = maxIndices[i];
      }
      markerIndices.push(Math.floor((start + end) / 2));
      i++;
    }

    return markerIndices.map(idx => ({
      type: 'Feature',
      geometry: features[idx].geometry,
      properties: {
        type: config.type,
        icon: config.icon,
        metric: config.metric,
        value: maxValue,
        idx
      }
    }));
  }

  function labelFor(metric){
    switch(metric){
      case 'speed': return 'Speed';
      case 'heartrate': return 'HR';
      case 'alt': return 'Alt';
      case 'grade': return 'Slope';
      case 'cadence': return 'Cadence';
      default: return metric;
    }
  }

  function fmtLegend(label){ return label; }
  function fmtValue(label,v){
    if (v==null||!isFinite(v)) return '—';
    switch(label){
      case 'Speed': return (v*3.6).toFixed(1)+' km/h';
      case 'HR': return Math.round(v)+' bpm';
      case 'Alt': return v.toFixed(0)+' m';
      case 'Slope': return v.toFixed(1)+'%';
      case 'Cadence': return Math.round(v)+' rpm';
      default: return String(v);
    }
  }

  function hrZonesExpression(zonesResponse, colors) {
    const zones = zonesResponse && zonesResponse.heart_rate && zonesResponse.heart_rate.zones || [];
    const expr = ['step', ['to-number', ['get','heartrate'], 0], colors[0]];
    for (let i = 1; i < zones.length && i < colors.length; i++) {
      const stop = zones[i].min || (zones[i-1].max+1);
      expr.push(stop, colors[i]);
    }
    return expr;
  }

  function solidLineGradient(color) {
    return ['interpolate', ['linear'], ['line-progress'], 0, color, 1, color];
  }

  function lineProgressGradientExpression(features, metric, min, max) {
    const expr = ['interpolate', ['linear'], ['line-progress']];
    for (const idx of sampledFeatureIndices(features)) {
      const progress = featureProgress(idx, features.length);
      const value = Number(features[idx]?.properties?.[metric]);
      expr.push(progress, metricColor(Number.isFinite(value) ? value : min, min, max));
    }
    ensureLineGradientEndpoints(expr, features, idx => {
      const value = Number(features[idx]?.properties?.[metric]);
      return metricColor(Number.isFinite(value) ? value : min, min, max);
    });
    return expr;
  }

  function movingLineGradient(features) {
    const expr = ['step', ['line-progress'], movingColor(features[0]?.properties?.moving)];
    for (const idx of sampledFeatureIndices(features).slice(1)) {
      expr.push(featureProgress(idx, features.length), movingColor(features[idx]?.properties?.moving));
    }
    return expr;
  }

  function hrZonesLineGradient(features, zones, colors) {
    const expr = ['step', ['line-progress'], hrZoneColor(features[0]?.properties?.heartrate, zones, colors)];
    for (const idx of sampledFeatureIndices(features).slice(1)) {
      expr.push(featureProgress(idx, features.length), hrZoneColor(features[idx]?.properties?.heartrate, zones, colors));
    }
    return expr;
  }

  function sampledFeatureIndices(features, maxStops = 256) {
    const count = Array.isArray(features) ? features.length : 0;
    if (count === 0) return [0];
    if (count <= maxStops) return Array.from({ length: count }, (_, i) => i);
    const step = (count - 1) / (maxStops - 1);
    const indices = [];
    let last = -1;
    for (let i = 0; i < maxStops; i++) {
      const idx = Math.min(count - 1, Math.round(i * step));
      if (idx !== last) {
        indices.push(idx);
        last = idx;
      }
    }
    if (indices[indices.length - 1] !== count - 1) indices.push(count - 1);
    return indices;
  }

  function featureProgress(index, count) {
    if (count <= 1) return 0;
    return Math.max(0, Math.min(1, index / (count - 1)));
  }

  function ensureLineGradientEndpoints(expr, features, colorForIndex) {
    const count = Array.isArray(features) ? features.length : 0;
    if (count === 0) {
      expr.push(0, '#7cc8ff', 1, '#7cc8ff');
      return;
    }
    if (expr.length === 3) {
      expr.push(0, colorForIndex(0), 1, colorForIndex(count - 1));
      return;
    }
    const firstStop = expr[3];
    if (firstStop !== 0) expr.splice(3, 0, 0, colorForIndex(0));
    const lastStop = expr[expr.length - 2];
    if (lastStop !== 1) expr.push(1, colorForIndex(count - 1));
  }

  function metricColor(value, min, max) {
    if (!Number.isFinite(value)) return '#7cc8ff';
    if (!Number.isFinite(min)) min = value;
    if (!Number.isFinite(max) || max <= min) max = min + 1;
    const t = Math.max(0, Math.min(1, (value - min) / (max - min)));
    if (t <= 0.5) return mixHex('#2ecc71', '#f1c40f', t / 0.5);
    return mixHex('#f1c40f', '#e74c3c', (t - 0.5) / 0.5);
  }

  function movingColor(moving) {
    return moving === false || moving === 'false' ? '#e74c3c' : '#4cc9f0';
  }

  function hrZoneColor(value, zones, colors) {
    const hr = Number(value);
    if (!Number.isFinite(hr)) return colors[0] || '#7cc8ff';
    const count = Math.min(Array.isArray(zones) ? zones.length : 0, colors.length);
    for (let i = count - 1; i >= 0; i--) {
      const min = Number(zones[i]?.min);
      if (Number.isFinite(min) && hr >= min) return colors[i];
    }
    return colors[0] || '#7cc8ff';
  }

  function mixHex(a, b, t) {
    const ca = hexToRgb(a);
    const cb = hexToRgb(b);
    const mix = channel => Math.round(ca[channel] + (cb[channel] - ca[channel]) * t);
    return `rgb(${mix('r')}, ${mix('g')}, ${mix('b')})`;
  }

  function hexToRgb(hex) {
    const value = hex.replace('#', '');
    return {
      r: parseInt(value.slice(0, 2), 16),
      g: parseInt(value.slice(2, 4), 16),
      b: parseInt(value.slice(4, 6), 16)
    };
  }

  function computeRange(features, metric) {
    let min = Infinity, max = -Infinity;
    for (const f of features) {
      let v = f.properties[metric];
      if (v == null) continue;
      if (metric === 'moving') v = v ? 0 : 1; // moving=true becomes 0 (green), moving=false becomes 1 (red)
      if (v < min) min = v;
      if (v > max) max = v;
    }
    if (!isFinite(min)) min = 0;
    if (!isFinite(max)) max = 1;
    if (min === max) { max = min + 1; }
    return {min, max};
  }

  function gradientExpression(metric, min, max) {
    return [
      'interpolate', ['linear'],
      ['to-number', ['get', metric], 0],
      min, '#2ecc71',
      (min + max) / 2, '#f1c40f',
      max, '#e74c3c'
    ];
  }

  function onIndexPage() {
    const form = document.getElementById('sync-form');
    const logEl = document.getElementById('sync-log');
    const progressEl = document.getElementById('sync-progress');
    const progressBar = document.getElementById('progress-bar');
    const progressText = document.getElementById('progress-text');
    const progressPhase = document.getElementById('progress-phase');
    const progressBarContainer = document.getElementById('progress-bar-container');
    
    if (!form || !logEl) return;
    
    let currentPhase = null;
    
    form.addEventListener('submit', (e) => {
      e.preventDefault();
      const fd = new FormData(form);
      const params = new URLSearchParams();
      const start = fd.get('start');
      const end = fd.get('end');
      if (start) params.set('start', start);
      if (end) params.set('end', end);
      const url = '/strava/sync' + (params.toString() ? ('?' + params.toString()) : '');
      logEl.style.display = 'block';
      logEl.textContent = '';
      // Force show progress elements
      if (progressEl) {
        progressEl.style.display = 'block';
        progressEl.style.visibility = 'visible';
        // Initialize progress bar - force visibility
        if (progressBarContainer) {
          progressBarContainer.style.display = 'block';
          progressBarContainer.style.visibility = 'visible';
          progressBarContainer.style.opacity = '1';
        }
        if (progressBar) {
          // Start at 0% when sync begins
          progressBar.style.width = '0%';
          progressBar.style.display = 'block';
          progressBar.style.visibility = 'visible';
          progressBar.style.opacity = '1';
        }
        if (progressPhase) {
          progressPhase.textContent = 'Starting sync...';
        }
        if (progressText) {
          progressText.textContent = '';
        }
      }
      currentPhase = null;
      
      const ev = new EventSource(url);
      ev.addEventListener('log', (m) => { logEl.textContent += m.data + "\n"; });
      ev.addEventListener('summary', (m) => { logEl.textContent += "Summary: " + m.data + "\n"; });
      ev.addEventListener('error', (m) => { logEl.textContent += "Error: " + m.data + "\n"; });
      ev.addEventListener('progress', (m) => {
        try {
          const data = JSON.parse(m.data);
          const phase = data.phase;
          const current = data.current || 0;
          const total = data.total || 1;
          const message = data.message || '';
          
          // Reset progress bar when phase changes
          if (phase !== currentPhase) {
            currentPhase = phase;
            if (progressBar) {
              progressBar.style.width = '0%';
            }
          }
          
          // Update progress bar based on phase
          let percentage = 0;
          
          if (phase === 'fetching_activities') {
            // Show 0% initially, then 100% when done
            if (total > 0 && current > 0 && current === total) {
              // Activities fetched, show 100%
              percentage = 100;
            } else if (total > 0 && current > 0) {
              // Partial progress (shouldn't happen for fetching_activities, but handle it)
              percentage = Math.round((current / total) * 100);
            } else {
              // Still fetching, show 0%
              percentage = 0;
            }
          } else if (phase === 'fetching_details') {
            // Reset to 0% when phase starts, then show done/total*100
            if (total > 0) {
              percentage = Math.round((current / total) * 100);
            } else {
              // No total yet, show 0%
              percentage = 0;
            }
          } else if (phase === 'saving') {
            // Reset to 0% when phase starts, then show done/total*100
            if (total > 0) {
              percentage = Math.round((current / total) * 100);
            } else {
              // No total yet, show 0%
              percentage = 0;
            }
          }
          
          // Clamp percentage to valid range
          percentage = Math.max(0, Math.min(100, percentage));
          
          if (progressBar) {
            progressBar.style.width = percentage + '%';
          }
          
          // Update phase label
          const phaseLabels = {
            'fetching_activities': 'Fetching activities',
            'fetching_details': 'Fetching details',
            'saving': 'Saving activities'
          };
          if (progressPhase) {
            progressPhase.textContent = phaseLabels[phase] || phase;
          }
          
          // Update progress text
          if (progressText) {
            if (total > 0) {
              progressText.textContent = `${current}/${total} (${percentage}%)`;
            } else {
              progressText.textContent = message || 'In progress...';
            }
          }
        } catch (e) {
          // Silently ignore parsing errors
        }
      });
      ev.addEventListener('done', () => { 
        ev.close(); 
        // Hide progress bar upon completion
        if (progressEl) {
          progressEl.style.display = 'none';
        }
        location.reload(); 
      });
      ev.onerror = () => { 
        ev.close(); 
        if (progressEl) {
          progressEl.style.display = 'none';
        }
      };
    });
  }

  function fmtSpeed(v){ return v!=null ? (v*3.6).toFixed(1)+" km/h" : '—'; }
  function fmtInt(v){ return v!=null ? Math.round(v) : '—'; }
  function fmtFloat(v){ return v!=null ? Number(v).toFixed(1) : '—'; }
  function fmtPct(v){ return v!=null ? Number(v).toFixed(1)+'%' : '—'; }
  function fmtTime(s){ try { return new Date(s).toLocaleString(); } catch { return '—'; } }

  // Global function for per-page selector
  window.changePerPage = function(value) {
    const url = new URL(window.location);
    url.searchParams.set('per_page', value);
    url.searchParams.set('page', '1'); // Reset to first page
    window.location.href = url.toString();
  };

  function onSegmentsPage() {
    const dashboard = document.getElementById('segments-dashboard');
    const filterInput = document.getElementById('segments-filter');
    const directionSelect = document.getElementById('segments-direction');
    const sortSelect = document.getElementById('segments-sort');
    let segmentCards = dashboard ? Array.from(dashboard.querySelectorAll('.segment-card[data-segment-id]')) : [];
    const deleteButtons = document.querySelectorAll('.delete-segment-btn');
    const deleteModal = document.getElementById('delete-modal');
    const deleteCancelBtn = document.getElementById('delete-cancel-btn');
    const deleteConfirmBtn = document.getElementById('delete-confirm-btn');
    const deleteSegmentName = document.getElementById('delete-segment-name');
    let segmentToDelete = null;

    const asNumber = (card, key) => {
      const value = Number(card.dataset[key]);
      return Number.isFinite(value) ? value : 0;
    };

    const applyDashboardControls = () => {
      if (!dashboard) return;
      const query = (filterInput?.value || '').trim().toLowerCase();
      const direction = directionSelect?.value || 'all';
      const sortBy = sortSelect?.value || 'name';

      const visible = segmentCards.filter(card => {
        const matchesText = !query || (card.dataset.name || '').includes(query);
        const matchesDirection = direction === 'all' || card.dataset.direction === direction;
        card.hidden = !(matchesText && matchesDirection);
        return !card.hidden;
      });

      visible.sort((a, b) => {
        switch (sortBy) {
        case 'attempts':
          return asNumber(b, 'attempts') - asNumber(a, 'attempts');
        case 'best':
          return asNumber(a, 'best') - asNumber(b, 'best');
        case 'worst':
          return asNumber(b, 'worst') - asNumber(a, 'worst');
        case 'minhr':
          return asNumber(a, 'minhr') - asNumber(b, 'minhr');
        case 'maxhr':
          return asNumber(b, 'maxhr') - asNumber(a, 'maxhr');
        case 'slope':
          return asNumber(b, 'slope') - asNumber(a, 'slope');
        case 'ascent':
          return asNumber(b, 'ascent') - asNumber(a, 'ascent');
        case 'direction':
          return (a.dataset.direction || '').localeCompare(b.dataset.direction || '') || (a.dataset.name || '').localeCompare(b.dataset.name || '');
        default:
          return (a.dataset.name || '').localeCompare(b.dataset.name || '');
        }
      });

      visible.forEach(card => dashboard.appendChild(card));
    };

    if (segmentCards.length > 0) {
      filterInput?.addEventListener('input', applyDashboardControls);
      directionSelect?.addEventListener('change', applyDashboardControls);
      sortSelect?.addEventListener('change', applyDashboardControls);
      applyDashboardControls();
    }

    if (deleteButtons.length > 0 && deleteModal && deleteCancelBtn && deleteConfirmBtn) {
      deleteButtons.forEach(btn => {
        btn.addEventListener('click', () => {
          const segmentId = btn.getAttribute('data-segment-id');
          const segmentName = btn.getAttribute('data-segment-name');
          segmentToDelete = segmentId;
          deleteSegmentName.textContent = segmentName;
          deleteModal.style.display = 'flex';
        });
      });

      deleteCancelBtn.addEventListener('click', () => {
        deleteModal.style.display = 'none';
        segmentToDelete = null;
      });

      deleteConfirmBtn.addEventListener('click', async () => {
        if (!segmentToDelete) return;

        try {
          const response = await fetch(`/api/segments/${segmentToDelete}`, {
            method: 'DELETE'
          });

          if (!response.ok) {
            const error = await response.text();
            throw new Error(error || 'Failed to delete segment');
          }

          // Remove the segment from the list
          const segmentItem = document.querySelector(`[data-segment-id="${segmentToDelete}"]`);
          if (segmentItem) {
            segmentItem.remove();
          }
          segmentCards = segmentCards.filter(card => card.getAttribute('data-segment-id') !== segmentToDelete);

          deleteModal.style.display = 'none';
          segmentToDelete = null;

          // If no segments left, show message
          const remainingSegments = document.querySelectorAll('.segment-card[data-segment-id]');
          if (remainingSegments.length === 0) {
            const list = document.querySelector('#segments-dashboard');
            if (list) {
              list.innerHTML = '<div class="item">No segments found. Create segments from activity pages.</div>';
            }
          }
        } catch (error) {
          alert('Error deleting segment: ' + error.message);
        }
      });

      // Close modal on background click
      deleteModal.addEventListener('click', (e) => {
        if (e.target === deleteModal) {
          deleteModal.style.display = 'none';
          segmentToDelete = null;
        }
      });
    }
  }

  function onSegmentPage() {
    const mapStyleURL = window.__MAP_STYLE_URL__;
    const segmentID = window.__SEGMENT_ID__;
    if (!mapStyleURL || !segmentID) return;
    
    const m = location.pathname.match(/\/segment\/(\d+)/);
    if (!m) return;
    
    const map = new maplibregl.Map({
      container: 'map',
      style: mapStyleURL,
      center: [0, 0],
      zoom: 2
    });
    installMissingStyleImageFallback(map);

    // Load segment geometry and display on map
    fetch(`/api/segments/${segmentID}`).then(r => r.json()).then(segment => {
      if (!segment || !segment.segment_geog) return;

      // Parse WKT LINESTRING
      const wkt = segment.segment_geog;
      const coordsMatch = wkt.match(/LINESTRING\s*\((.+)\)/i);
      if (!coordsMatch) {
        console.warn('Failed to parse segment WKT:', wkt);
        return;
      }

      const coords = coordsMatch[1].split(',').map(s => {
        const parts = s.trim().split(/\s+/);
        const lng = parseFloat(parts[0]);
        const lat = parseFloat(parts[1]);
        if (isNaN(lng) || isNaN(lat)) {
          console.warn('Invalid coordinate:', s);
          return null;
        }
        return [lng, lat]; // [lng, lat]
      }).filter(c => c !== null);
      
      if (coords.length === 0) {
        console.warn('No valid coordinates found in segment');
        return;
      }

      map.on('load', () => {
        // Add segment line
        map.addSource('segment', {
          type: 'geojson',
          data: {
            type: 'Feature',
            geometry: { type: 'LineString', coordinates: coords }
          }
        });
        map.addLayer({
          id: 'segment-line',
          type: 'line',
          source: 'segment',
          paint: { 'line-color': '#f1c40f', 'line-width': 4 }
        });

        // Fit bounds
        const bounds = new maplibregl.LngLatBounds();
        for (const c of coords) bounds.extend(c);
        if (!bounds.isEmpty()) map.fitBounds(bounds, { padding: 40, duration: 0 });
        
        // Calculate and display segment metrics
        calculateSegmentMetrics(coords);
      });
    });
    
    function calculateSegmentMetrics(coords) {
      if (coords.length < 2) return;
      
      // Calculate distance using Haversine formula
      let totalDistance = 0;
      let minAlt = Infinity;
      let maxAlt = -Infinity;
      
      for (let i = 0; i < coords.length - 1; i++) {
        const [lng1, lat1] = coords[i];
        const [lng2, lat2] = coords[i + 1];
        
        // Haversine distance
        const R = 6371000; // Earth radius in meters
        const dLat = (lat2 - lat1) * Math.PI / 180;
        const dLng = (lng2 - lng1) * Math.PI / 180;
        const a = Math.sin(dLat/2) * Math.sin(dLat/2) +
                  Math.cos(lat1 * Math.PI / 180) * Math.cos(lat2 * Math.PI / 180) *
                  Math.sin(dLng/2) * Math.sin(dLng/2);
        const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1-a));
        totalDistance += R * c;
      }
      
        // Fetch segment metrics from API
      fetch(`/api/segments/${segmentID}/metrics`)
        .then(r => {
          if (!r.ok) {
            throw new Error(`HTTP ${r.status}: ${r.statusText}`);
          }
          return r.json();
        })
        .then(metrics => {
          const distanceEl = document.getElementById('segment-distance');
          const elevationEl = document.getElementById('segment-elevation');
          
          if (distanceEl) {
            distanceEl.textContent = metrics.distance ? `${(metrics.distance / 1000).toFixed(2)} km` : `${(totalDistance / 1000).toFixed(2)} km`;
          }
          if (elevationEl) {
            if (metrics.elevation_gain && metrics.elevation_gain > 0) {
              elevationEl.textContent = `${Math.round(metrics.elevation_gain)} m`;
            } else {
              elevationEl.textContent = 'N/A';
            }
          }
        })
        .catch(err => {
          console.error('Error loading segment metrics:', err);
          // Fallback to calculated distance
          const distanceEl = document.getElementById('segment-distance');
          if (distanceEl) {
            distanceEl.textContent = `${(totalDistance / 1000).toFixed(2)} km`;
          }
          const elevationEl = document.getElementById('segment-elevation');
          if (elevationEl) {
            elevationEl.textContent = 'N/A';
          }
        });
    }

    // Find activities button
    const findBtn = document.getElementById('find-activities-btn');
    const refreshBtn = document.getElementById('refresh-cache-btn');
    const toleranceInput = document.getElementById('tolerance');
    const sortSelect = document.getElementById('sort-by');
    const activitiesSection = document.getElementById('activities-section');
    const activitiesList = document.getElementById('activities-list');
    const activitiesLoading = document.getElementById('activities-loading');
    let selectedActivityID = null;
    let currentActivityFeatures = null; // Store features for color metric changes
    const selectedEfforts = new Map();
    const compareColors = ['#70d6ff', '#f5d76e', '#c084fc'];

    const escapeHtml = value => String(value ?? '').replace(/[&<>"']/g, ch => ({
      '&': '&amp;',
      '<': '&lt;',
      '>': '&gt;',
      '"': '&quot;',
      "'": '&#39;'
    }[ch]));

    const secondsValue = value => Number.isFinite(Number(value)) && Number(value) > 0 ? Number(value) : null;
    const speedValue = activity => Number(activity.segment_avg_speed || activity.average_speed || 0);
    const hrValue = activity => Number(activity.segment_avg_hr || activity.average_heartrate || 0);
    const effortDate = activity => {
      const raw = activity.start_date_formatted || activity.start_date;
      const date = raw ? new Date(raw) : null;
      return date && !Number.isNaN(date.getTime()) ? date : null;
    };
    const formatEffortDate = activity => {
      const date = effortDate(activity);
      return date ? date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' }) : 'Unknown';
    };
    const formatDuration = seconds => {
      const value = secondsValue(seconds);
      if (value === null) return 'n/a';
      const total = Math.round(value);
      const h = Math.floor(total / 3600);
      const m = Math.floor((total % 3600) / 60);
      const s = total % 60;
      if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
      return `${m}:${String(s).padStart(2, '0')}`;
    };
    const formatDelta = seconds => {
      if (!Number.isFinite(seconds)) return 'n/a';
      const sign = seconds > 0 ? '+' : seconds < 0 ? '-' : '';
      return `${sign}${formatDuration(Math.abs(seconds))}`;
    };
    const enrichEfforts = activities => {
      const withTime = activities.filter(activity => secondsValue(activity.segment_elapsed_seconds) !== null);
      const bestTime = withTime.length > 0 ? Math.min(...withTime.map(activity => secondsValue(activity.segment_elapsed_seconds))) : null;
      const chronological = [...withTime].sort((a, b) => {
        const ad = effortDate(a)?.getTime() || 0;
        const bd = effortDate(b)?.getTime() || 0;
        return ad - bd;
      });
      const previousByID = new Map();
      for (let i = 1; i < chronological.length; i++) {
        const current = chronological[i];
        const previous = chronological[i - 1];
        previousByID.set(current.id, secondsValue(current.segment_elapsed_seconds) - secondsValue(previous.segment_elapsed_seconds));
      }

      return activities.map(activity => {
        const effortSeconds = secondsValue(activity.segment_elapsed_seconds);
        return {
          ...activity,
          effortSeconds,
          deltaBest: effortSeconds !== null && bestTime !== null ? effortSeconds - bestTime : null,
          deltaPrevious: previousByID.has(activity.id) ? previousByID.get(activity.id) : null
        };
      });
    };

    const renderEffortSummary = efforts => {
      const timed = efforts.filter(effort => effort.effortSeconds !== null);
      const best = timed.length > 0 ? timed.reduce((winner, effort) => effort.effortSeconds < winner.effortSeconds ? effort : winner, timed[0]) : null;
      const latest = timed.length > 0 ? timed.reduce((winner, effort) => {
        const effortTime = effortDate(effort)?.getTime() || 0;
        const winnerTime = effortDate(winner)?.getTime() || 0;
        return effortTime > winnerTime ? effort : winner;
      }, timed[0]) : null;
      const avgHr = timed.map(hrValue).filter(Boolean);
      const avgHrValue = avgHr.length > 0 ? Math.round(avgHr.reduce((sum, value) => sum + value, 0) / avgHr.length) : null;

      return `
        <div class="effort-summary-grid">
          <div class="stat-card">
            <span class="stat-label">Efforts</span>
            <strong>${efforts.length}</strong>
          </div>
          <div class="stat-card">
            <span class="stat-label">Best</span>
            <strong>${best ? formatDuration(best.effortSeconds) : 'n/a'}</strong>
          </div>
          <div class="stat-card">
            <span class="stat-label">Avg HR</span>
            <strong>${avgHrValue ? `${avgHrValue} bpm` : 'n/a'}</strong>
          </div>
        </div>
        ${latest ? `<div class="meta">Latest effort: ${formatEffortDate(latest)} (${formatDuration(latest.effortSeconds)})</div>` : ''}
      `;
    };

    const renderZoneMini = zones => {
      if (!Array.isArray(zones) || zones.length === 0) return '<span class="muted">n/a</span>';
      return `
        <div class="zone-mini">
          ${zones.map(zone => `
            <span title="${escapeHtml(zone.label || `Z${zone.zone}`)} ${Number(zone.percentage || 0).toFixed(1)}%">
              <i style="height:${Math.max(2, Number(zone.percentage || 0))}%"></i>
            </span>
          `).join('')}
        </div>
      `;
    };

    function loadActivities(forceRefresh = false) {
      const tolerance = parseFloat(toleranceInput.value) || 15;
      const sortBy = sortSelect.value || 'distance';
      const refreshParam = forceRefresh ? '&refresh=true' : '';

      activitiesLoading.style.display = 'block';
      activitiesSection.style.display = 'none';

      fetch(`/api/segments/${segmentID}/activities?tolerance=${tolerance}&sort=${sortBy}${refreshParam}`)
        .then(r => {
          if (!r.ok) throw new Error(`HTTP ${r.status}: ${r.statusText}`);
          return r.json();
        })
        .then(activities => {
          activitiesLoading.style.display = 'none';
          activitiesSection.style.display = 'block';

          if (activities.length === 0) {
            activitiesList.innerHTML = '<div class="muted">No same-direction efforts found for this segment.</div>';
            return;
          }

          const efforts = enrichEfforts(activities);
          activitiesList.innerHTML = `
            ${renderEffortSummary(efforts)}
            <div class="effort-table-wrap">
              <table class="effort-table">
                <thead>
                  <tr>
                    <th>Compare</th>
                    <th>Effort</th>
                    <th>Time</th>
                    <th>HR</th>
                    <th>Speed</th>
                    <th>HR zones</th>
                    <th>Δ best</th>
                    <th>Δ prev</th>
                    <th>Match</th>
                  </tr>
                </thead>
                <tbody>
                  ${efforts.map(activity => {
                    const speed = speedValue(activity);
                    const hr = hrValue(activity);
                    const deltaBestClass = activity.deltaBest === 0 ? 'delta-good' : 'delta-slow';
                    const deltaPrevClass = activity.deltaPrevious !== null && activity.deltaPrevious <= 0 ? 'delta-good' : 'delta-slow';
                    return `
                      <tr class="effort-row ${selectedEfforts.has(activity.id) ? 'selected' : ''}" data-activity-id="${activity.id}">
                        <td><button type="button" class="compare-toggle" data-activity-id="${activity.id}">${selectedEfforts.has(activity.id) ? 'On' : 'Add'}</button></td>
                        <td>
                          <span class="effort-name">${escapeHtml(activity.name || 'Activity')}</span>
                          <span class="meta">${formatEffortDate(activity)} · <a class="link" href="/activity/${activity.id}">Open</a></span>
                        </td>
                        <td>${formatDuration(activity.effortSeconds)}</td>
                        <td>${hr > 0 ? `${Math.round(hr)}` : 'n/a'}</td>
                        <td>${speed > 0 ? `${(speed * 3.6).toFixed(1)}` : 'n/a'}</td>
                        <td>${renderZoneMini(activity.segment_hr_zones)}</td>
                        <td class="${deltaBestClass}">${activity.deltaBest === null ? 'n/a' : formatDelta(activity.deltaBest)}</td>
                        <td class="${deltaPrevClass}">${activity.deltaPrevious === null ? 'n/a' : formatDelta(activity.deltaPrevious)}</td>
                        <td>${activity.overlap_percentage ? `${activity.overlap_percentage.toFixed(0)}%` : 'n/a'}</td>
                      </tr>
                    `;
                  }).join('')}
                </tbody>
              </table>
            </div>
          `;

          // Add click handlers
          activitiesList.querySelectorAll('.effort-row[data-activity-id]').forEach(row => {
            row.addEventListener('click', (e) => {
              // Don't navigate if clicking on the "View Full" link
              if (e.target.tagName === 'A') return;
              if (e.target.closest('button')) return;
              
              const activityID = parseInt(row.getAttribute('data-activity-id'));
              const activity = efforts.find(item => item.id === activityID);
              toggleEffortComparison(activity, tolerance);
            });
          });
          activitiesList.querySelectorAll('.compare-toggle[data-activity-id]').forEach(btn => {
            btn.addEventListener('click', () => {
              const activityID = parseInt(btn.getAttribute('data-activity-id'));
              const activity = efforts.find(item => item.id === activityID);
              toggleEffortComparison(activity, tolerance);
            });
          });
        })
        .catch(err => {
          activitiesLoading.style.display = 'none';
          activitiesSection.style.display = 'block';
          activitiesList.innerHTML = `<div class="delta-slow">Error loading efforts: ${escapeHtml(err.message)}</div>`;
        });
    }

    function repaintEffortSelection() {
      activitiesList.querySelectorAll('.effort-row[data-activity-id]').forEach(row => {
        const activityID = parseInt(row.getAttribute('data-activity-id'));
        const index = Array.from(selectedEfforts.keys()).indexOf(activityID);
        row.classList.toggle('selected', index >= 0);
        row.style.setProperty('--effort-color', index >= 0 ? compareColors[index] : 'transparent');
      });
      activitiesList.querySelectorAll('.compare-toggle[data-activity-id]').forEach(btn => {
        const activityID = parseInt(btn.getAttribute('data-activity-id'));
        btn.textContent = selectedEfforts.has(activityID) ? 'On' : 'Add';
      });
    }

    function toggleEffortComparison(activity, tolerance) {
      if (!activity) return;
      if (selectedEfforts.has(activity.id)) {
        selectedEfforts.delete(activity.id);
      } else {
        if (selectedEfforts.size >= 3) {
          alert('Select up to three efforts to compare.');
          return;
        }
        selectedEfforts.set(activity.id, activity);
      }

      selectedActivityID = selectedEfforts.size > 0 ? Array.from(selectedEfforts.keys())[selectedEfforts.size - 1] : null;
      repaintEffortSelection();
      renderSelectedEffortsOnMap(tolerance);
      updateSegmentComparisonGraph();
    }

    function clearComparisonMapLayers() {
      for (let i = 0; i < 3; i++) {
        const lineLayer = `comparison-effort-line-${i}`;
        const pointLayer = `comparison-effort-points-${i}`;
        const lineSource = `comparison-effort-${i}`;
        const pointSource = `comparison-effort-points-${i}`;
        if (map.getLayer(pointLayer)) map.removeLayer(pointLayer);
        if (map.getSource(pointSource)) map.removeSource(pointSource);
        if (map.getLayer(lineLayer)) map.removeLayer(lineLayer);
        if (map.getSource(lineSource)) map.removeSource(lineSource);
      }
    }

    function fetchSegmentEffort(activityID, segID, tolerance) {
      return Promise.all([
        fetch(`/api/activities/${activityID}/points`).then(r => r.json()),
        fetch(`/api/segments/${segID}/activity/${activityID}/indices?tolerance=${tolerance}`).then(r => r.json())
      ]).then(([points, indices]) => {
        if (!Array.isArray(points) || points.length === 0) return null;
        const startIdx = indices.start_index || 0;
        const endIdx = indices.end_index || points.length - 1;
        const segmentPoints = points.slice(startIdx, endIdx + 1);
        if (segmentPoints.length === 0) return null;

        const lineCoords = segmentPoints.map(p => [p.lng, p.lat]);
        const features = segmentPoints.map((p, idx) => ({
          type: 'Feature',
          geometry: { type: 'Point', coordinates: [p.lng, p.lat] },
          properties: { idx: startIdx + idx, time: p.time, speed: p.speed, cadence: p.cadence, heartrate: p.heartrate, alt: p.altitude, grade: p.grade, moving: p.moving }
        }));
        const lineFeature = {
          type: 'Feature',
          geometry: { type: 'LineString', coordinates: lineCoords },
          properties: { activity_id: activityID }
        };
        return { activityID, points: segmentPoints, features, lineFeature };
      });
    }

    function renderSelectedEffortsOnMap(tolerance) {
      clearComparisonMapLayers();
      const selections = Array.from(selectedEfforts.keys());
      if (selections.length === 0) {
        currentActivityFeatures = null;
        return;
      }

      Promise.all(selections.map(activityID => fetchSegmentEffort(activityID, segmentID, tolerance)))
        .then(efforts => {
          efforts.filter(Boolean).forEach((effort, index) => {
            const color = compareColors[index];
            const lineSource = `comparison-effort-${index}`;
            const lineLayer = `comparison-effort-line-${index}`;
            const pointSource = `comparison-effort-points-${index}`;
            const pointLayer = `comparison-effort-points-${index}`;

            map.addSource(lineSource, {
              type: 'geojson',
              data: { type: 'FeatureCollection', features: [effort.lineFeature] }
            });
            map.addLayer({
              id: lineLayer,
              type: 'line',
              source: lineSource,
              paint: { 'line-color': color, 'line-width': 4 + index, 'line-opacity': index === efforts.length - 1 ? 0.95 : 0.68 }
            });

            map.addSource(pointSource, {
              type: 'geojson',
              data: { type: 'FeatureCollection', features: effort.features }
            });
            map.addLayer({
              id: pointLayer,
              type: 'circle',
              source: pointSource,
              paint: { 'circle-radius': 3, 'circle-color': color, 'circle-opacity': 0.2 }
            });

            if (index === efforts.length - 1) {
              currentActivityFeatures = effort.features;
            }
          });
        })
        .catch(err => console.error('Failed to render comparison efforts:', err));
    }

    function loadActivityPoints(activityID, segID, tolerance, preserveColorMetric = null) {
      fetch(`/api/activities/${activityID}/points`).then(r => r.json()).then(points => {
        if (!Array.isArray(points) || points.length === 0) return;

        // Get segment portion indices
        fetch(`/api/segments/${segID}/activity/${activityID}/indices?tolerance=${tolerance}`)
          .then(r => r.json())
          .then(indices => {
            const startIdx = indices.start_index || 0;
            const endIdx = indices.end_index || points.length;
            
            // Filter points to segment portion
            const segmentPoints = points.slice(startIdx, endIdx + 1);
            
            if (segmentPoints.length === 0) return;

            const lineCoords = segmentPoints.map(p => [p.lng, p.lat]);
            const features = segmentPoints.map((p, idx) => ({
              type: 'Feature',
              geometry: { type: 'Point', coordinates: [p.lng, p.lat] },
              properties: { idx: startIdx + idx, time: p.time, speed: p.speed, cadence: p.cadence, heartrate: p.heartrate, alt: p.altitude, grade: p.grade, moving: p.moving }
            }));
            const fc = { type: 'FeatureCollection', features };

            // Create line segments for gradient coloring
            const lineSegments = [];
            for (let i = 0; i < lineCoords.length - 1; i++) {
              lineSegments.push({
                type: 'Feature',
                geometry: {
                  type: 'LineString',
                  coordinates: [lineCoords[i], lineCoords[i + 1]]
                },
                properties: {
                  idx: startIdx + i,
                  speed: ((features[i].properties?.speed || 0) + (features[i + 1]?.properties?.speed || 0)) / 2,
                  heartrate: ((features[i].properties?.heartrate || 0) + (features[i + 1]?.properties?.heartrate || 0)) / 2,
                  cadence: ((features[i].properties?.cadence || 0) + (features[i + 1]?.properties?.cadence || 0)) / 2,
                  alt: ((features[i].properties?.alt || 0) + (features[i + 1]?.properties?.alt || 0)) / 2,
                  grade: ((features[i].properties?.grade || 0) + (features[i + 1]?.properties?.grade || 0)) / 2,
                  moving: features[i].properties?.moving || false
                }
              });
            }

            // Remove existing activity points if any
            if (map.getSource('activity-points')) {
              map.removeLayer('activity-points-layer');
              map.removeSource('activity-points');
            }

            // Add activity route line (full route, dimmed)
            if (map.getSource('activity-route')) {
              map.removeLayer('activity-route-line');
              map.removeSource('activity-route');
            }

            const fullLineCoords = points.map(p => [p.lng, p.lat]);
            map.addSource('activity-route', {
              type: 'geojson',
              data: { type: 'Feature', geometry: { type: 'LineString', coordinates: fullLineCoords } }
            });
            map.addLayer({
              id: 'activity-route-line',
              type: 'line',
              source: 'activity-route',
              paint: { 'line-color': '#4cc9f0', 'line-width': 2, 'line-opacity': 0.3 }
            });

            // Add segment portion line (highlighted, with gradient segments)
            if (map.getSource('activity-segment-portion')) {
              map.removeLayer('activity-segment-portion-line');
              map.removeSource('activity-segment-portion');
            }
            map.addSource('activity-segment-portion', {
              type: 'geojson',
              data: { type: 'FeatureCollection', features: lineSegments }
            });
            map.addLayer({
              id: 'activity-segment-portion-line',
              type: 'line',
              source: 'activity-segment-portion',
              paint: { 'line-color': '#4cc9f0', 'line-width': 4, 'line-opacity': 0.8 }
            });

            // Add activity points (only segment portion)
            map.addSource('activity-points', { type: 'geojson', data: fc });
            map.addLayer({
              id: 'activity-points-layer',
              type: 'circle',
              source: 'activity-points',
              paint: { 'circle-radius': 4, 'circle-color': '#f72585', 'circle-opacity': 1 }
            });

            // Add click handler for point popup
            if (!map._activityPointsPopupHandler) {
              const popup = new maplibregl.Popup({ closeButton: true, closeOnClick: true, className: 'point-popup' });
              map.on('click', 'activity-points-layer', (e) => {
                const f = e.features && e.features[0];
                if (!f) return;
                const p = f.properties;
                const html = `Speed: ${fmtSpeed(p.speed)}<br/>Cadence: ${fmtInt(p.cadence)}<br/>HR: ${fmtInt(p.heartrate)}<br/>Alt: ${fmtFloat(p.alt)} m<br/>Grade: ${fmtPct(p.grade)}<br/>${p.moving ? 'In motion' : 'Stopped'}<br/>Time: ${fmtTime(p.time)}`;
                popup.setLngLat(e.lngLat).setHTML(html).addTo(map);
              });
              map.on('mouseenter', 'activity-points-layer', () => map.getCanvas().style.cursor = 'pointer');
              map.on('mouseleave', 'activity-points-layer', () => map.getCanvas().style.cursor = '');
              map._activityPointsPopupHandler = true;
            }

            // Store features globally for color metric changes
            currentActivityFeatures = features;
            
            // Apply color metric immediately, preserving selection if provided
            if (preserveColorMetric) {
              const select = document.getElementById('color-metric');
              if (select) {
                select.value = preserveColorMetric;
              }
            }
            applyColorMetric(features);
            
            // Update graph when activity is selected
            updateSegmentGraph(activityID, segmentID);
          })
          .catch(err => {
            console.warn('Failed to get segment indices, showing full activity:', err);
            // Fallback: show full activity if we can't get segment portion
            const lineCoords = points.map(p => [p.lng, p.lat]);
            const features = points.map((p, idx) => ({
              type: 'Feature',
              geometry: { type: 'Point', coordinates: [p.lng, p.lat] },
              properties: { idx, time: p.time, speed: p.speed, cadence: p.cadence, heartrate: p.heartrate, alt: p.altitude, grade: p.grade, moving: p.moving }
            }));
            const fc = { type: 'FeatureCollection', features };

            // Create line segments for gradient coloring
            const lineSegments = [];
            for (let i = 0; i < lineCoords.length - 1; i++) {
              lineSegments.push({
                type: 'Feature',
                geometry: {
                  type: 'LineString',
                  coordinates: [lineCoords[i], lineCoords[i + 1]]
                },
                properties: {
                  idx: i,
                  speed: ((features[i].properties?.speed || 0) + (features[i + 1]?.properties?.speed || 0)) / 2,
                  heartrate: ((features[i].properties?.heartrate || 0) + (features[i + 1]?.properties?.heartrate || 0)) / 2,
                  cadence: ((features[i].properties?.cadence || 0) + (features[i + 1]?.properties?.cadence || 0)) / 2,
                  alt: ((features[i].properties?.alt || 0) + (features[i + 1]?.properties?.alt || 0)) / 2,
                  grade: ((features[i].properties?.grade || 0) + (features[i + 1]?.properties?.grade || 0)) / 2,
                  moving: features[i].properties?.moving || false
                }
              });
            }

            if (map.getSource('activity-points')) {
              map.removeLayer('activity-points-layer');
              map.removeSource('activity-points');
            }
            if (map.getSource('activity-route')) {
              map.removeLayer('activity-route-line');
              map.removeSource('activity-route');
            }

            map.addSource('activity-route', {
              type: 'geojson',
              data: { type: 'FeatureCollection', features: lineSegments }
            });
            map.addLayer({
              id: 'activity-route-line',
              type: 'line',
              source: 'activity-route',
              paint: { 'line-color': '#4cc9f0', 'line-width': 2, 'line-opacity': 0.6 }
            });

            map.addSource('activity-points', { type: 'geojson', data: fc });
            map.addLayer({
              id: 'activity-points-layer',
              type: 'circle',
              source: 'activity-points',
              paint: { 'circle-radius': 3, 'circle-color': '#f72585' }
            });

            // Add click handler for point popup (if not already added)
            if (!map._activityPointsPopupHandler) {
              const popup = new maplibregl.Popup({ closeButton: true, closeOnClick: true, className: 'point-popup' });
              map.on('click', 'activity-points-layer', (e) => {
                const f = e.features && e.features[0];
                if (!f) return;
                const p = f.properties;
                const html = `Speed: ${fmtSpeed(p.speed)}<br/>Cadence: ${fmtInt(p.cadence)}<br/>HR: ${fmtInt(p.heartrate)}<br/>Alt: ${fmtFloat(p.alt)} m<br/>Grade: ${fmtPct(p.grade)}<br/>${p.moving ? 'In motion' : 'Stopped'}<br/>Time: ${fmtTime(p.time)}`;
                popup.setLngLat(e.lngLat).setHTML(html).addTo(map);
              });
              map.on('mouseenter', 'activity-points-layer', () => map.getCanvas().style.cursor = 'pointer');
              map.on('mouseleave', 'activity-points-layer', () => map.getCanvas().style.cursor = '');
              map._activityPointsPopupHandler = true;
            }

            // Store features globally for color metric changes
            currentActivityFeatures = features;
            
            // Preserve color metric selection
            if (preserveColorMetric) {
              const select = document.getElementById('color-metric');
              if (select) {
                select.value = preserveColorMetric;
              }
            }
            applyColorMetric(features);
            
            // Update graph when activity is selected (fallback case)
            if (selectedActivityID) {
              updateSegmentGraph(selectedActivityID, segmentID);
            }
          });
      });
    }

    // Graph rendering functionality for segment page
    let segmentChartInstance = null;
    let segmentGraphPoints = null;
    const metric1Select = document.getElementById('metric1-select');
    const metric2Select = document.getElementById('metric2-select');
    const xAxisSelect = document.getElementById('xaxis-select');
    const graphCanvas = document.getElementById('graph-canvas');
    const graphContainer = document.getElementById('graph-container');
    
    // Helper function to calculate cumulative distance from points (same as activity page)
    const calculateCumulativeDistance = (points) => {
      if (!points || points.length < 2) return [];
      
      const distances = [0]; // First point has 0 distance
      const R = 6371000; // Earth radius in meters
      
      for (let i = 1; i < points.length; i++) {
        const prev = points[i - 1];
        const curr = points[i];
        
        if (!prev.lat || !prev.lng || !curr.lat || !curr.lng) {
          distances.push(distances[i - 1]); // Use previous distance if coordinates missing
          continue;
        }
        
        const dLat = (curr.lat - prev.lat) * Math.PI / 180;
        const dLng = (curr.lng - prev.lng) * Math.PI / 180;
        const a = Math.sin(dLat/2) * Math.sin(dLat/2) +
                   Math.cos(prev.lat * Math.PI / 180) * Math.cos(curr.lat * Math.PI / 180) *
                   Math.sin(dLng/2) * Math.sin(dLng/2);
        const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1-a));
        const distance = R * c;
        
        distances.push(distances[i - 1] + distance);
      }
      
      return distances; // Returns distances in meters
    };

    function metricLabel(metric) {
      if (metric === 'heartrate') return 'HR';
      if (metric === 'height') return 'Elevation';
      return metric.charAt(0).toUpperCase() + metric.slice(1);
    }

    function convertGraphValue(value, metric) {
      return metric === 'speed' ? value * 3.6 : value;
    }

    function updateSegmentComparisonGraph() {
      if (!metric1Select || !metric2Select || !graphCanvas) return;
      const selected = Array.from(selectedEfforts.values());
      if (selected.length === 0) {
        selectedActivityID = null;
        if (segmentChartInstance) {
          segmentChartInstance.destroy();
          segmentChartInstance = null;
        }
        const placeholder = document.getElementById('graph-placeholder');
        if (graphContainer) graphContainer.classList.add('graph-empty');
        if (graphCanvas) graphCanvas.style.display = 'none';
        if (placeholder) placeholder.style.display = 'block';
        return;
      }

      const metric1 = metric1Select.value;
      const metric2 = metric2Select.value;
      const xAxisType = xAxisSelect ? xAxisSelect.value : 'time';
      const metrics = [metric1, metric2].filter(Boolean);
      const placeholder = document.getElementById('graph-placeholder');

      if (metrics.length === 0) {
        if (segmentChartInstance) {
          segmentChartInstance.destroy();
          segmentChartInstance = null;
        }
        if (graphContainer) graphContainer.classList.add('graph-empty');
        if (graphCanvas) graphCanvas.style.display = 'none';
        if (placeholder) placeholder.style.display = 'block';
        return;
      }

      if (graphContainer) graphContainer.classList.remove('graph-empty');
      if (placeholder) placeholder.style.display = 'none';
      graphCanvas.style.display = 'block';
      graphCanvas.style.width = '100%';
      graphCanvas.style.height = '250px';

      Promise.all(selected.map((activity, effortIndex) => {
        const includeZones = metrics.includes('heartrate');
        const url = `/api/segments/${segmentID}/graph?metrics=${metrics.join(',')}&activity_id=${activity.id}&include_zones=${includeZones}`;
        return fetch(url)
          .then(r => {
            if (!r.ok) throw new Error(`Graph fetch failed for ${activity.name}`);
            return r.json();
          })
          .then(data => ({ activity, effortIndex, data }));
      })).then(results => {
        const datasets = [];

        results.forEach(({ activity, effortIndex, data }) => {
          metrics.forEach((metric, metricIndex) => {
            const metricData = data[metric];
            if (!Array.isArray(metricData) || metricData.length === 0) return;
            const firstTime = new Date(metricData[0].time).getTime();
            const firstDistance = metricData[0].distance || 0;
            const baseColor = compareColors[effortIndex];
            datasets.push({
              label: `${activity.name || 'Effort'} · ${metricLabel(metric)}`,
              data: metricData.map(point => {
                let x;
                if (xAxisType === 'distance' && point.distance != null) {
                  x = Math.max(0, (point.distance - firstDistance) / 1000);
                } else {
                  x = Math.max(0, (new Date(point.time).getTime() - firstTime) / 1000);
                }
                return {
                  x,
                  y: convertGraphValue(point.value, metric),
                  time: point.time
                };
              }),
              borderColor: baseColor,
              backgroundColor: 'transparent',
              borderWidth: metricIndex === 0 ? 2.6 : 1.8,
              borderDash: metricIndex === 0 ? [] : [5, 5],
              pointRadius: 0,
              pointHoverRadius: 4,
              tension: 0.18,
              yAxisID: metricIndex === 0 ? 'y' : 'y1'
            });
          });
        });

        if (segmentChartInstance) {
          segmentChartInstance.destroy();
          segmentChartInstance = null;
        }

        const ctx = graphCanvas.getContext('2d');
        segmentChartInstance = new Chart(ctx, {
          type: 'line',
          data: { datasets },
          options: {
            responsive: true,
            maintainAspectRatio: false,
            interaction: { intersect: false, mode: 'nearest' },
            plugins: {
              legend: {
                display: true,
                position: 'top',
                labels: { color: '#e0e0e0', boxWidth: 28 }
              },
              tooltip: {
                callbacks: {
                  title: context => {
                    const x = context[0].parsed.x;
                    return xAxisType === 'distance' ? `${x.toFixed(2)} km` : formatDuration(x);
                  },
                  label: context => {
                    const label = context.dataset.label || '';
                    const value = context.parsed.y;
                    const unit = label.includes('Speed') ? ' km/h' : label.includes('HR') ? ' bpm' : label.includes('Cadence') ? ' rpm' : label.includes('Elevation') ? ' m' : '';
                    return `${label}: ${value.toFixed(1)}${unit}`;
                  }
                }
              }
            },
            scales: {
              x: {
                type: 'linear',
                title: {
                  display: true,
                  text: xAxisType === 'distance' ? 'Segment distance (km)' : 'Segment elapsed time',
                  color: '#e0e0e0'
                },
                ticks: {
                  color: '#e0e0e0',
                  callback: value => xAxisType === 'distance' ? `${Number(value).toFixed(1)} km` : formatDuration(value)
                },
                grid: { color: '#333' }
              },
              y: {
                position: 'left',
                ticks: { color: '#e0e0e0' },
                grid: { color: '#333' }
              },
              y1: {
                type: 'linear',
                display: metrics.length > 1,
                position: 'right',
                ticks: { color: '#e0e0e0' },
                grid: { drawOnChartArea: false }
              }
            }
          }
        });
      }).catch(error => {
        console.error('Error loading comparison graph:', error);
      });
    }

    function updateSegmentGraph(activityID, segID) {
      if (!metric1Select || !metric2Select || !graphCanvas || !activityID) return;
      
      const metric1 = metric1Select.value;
      const metric2 = metric2Select.value;
      const xAxisType = xAxisSelect ? xAxisSelect.value : 'time';
      
      const placeholder = document.getElementById('graph-placeholder');
      
      if (!metric1 && !metric2) {
        if (segmentChartInstance) {
          segmentChartInstance.destroy();
          segmentChartInstance = null;
        }
        if (graphContainer) graphContainer.classList.add('graph-empty');
        if (graphCanvas) graphCanvas.style.display = 'none';
        if (placeholder) placeholder.style.display = 'block';
        // Keep container visible so users can select metrics
        return;
      }
      
      // Hide placeholder and show canvas
      if (graphContainer) graphContainer.classList.remove('graph-empty');
      if (placeholder) placeholder.style.display = 'none';
      if (graphCanvas) {
        graphCanvas.style.display = 'block';
        graphCanvas.style.width = '100%';
        graphCanvas.style.height = '250px';
      }
      
      const metrics = [];
      if (metric1) metrics.push(metric1);
      if (metric2) metrics.push(metric2);
      
      const includeZones = metric1 === 'heartrate' || metric2 === 'heartrate';
      const url = `/api/segments/${segID}/graph?metrics=${metrics.join(',')}&activity_id=${activityID}&include_zones=${includeZones}`;
      
      fetch(url)
        .then(r => {
          if (!r.ok) throw new Error('Failed to fetch graph data');
          return r.json();
        })
        .then(data => {
          // Store points for synchronization (fetch from activity points)
          fetch(`/api/activities/${activityID}/points`)
            .then(r => r.json())
            .then(points => {
              segmentGraphPoints = points;
            })
            .catch(() => {});
          
          // Use same graph rendering logic as activity page
          const datasets = [];
          
          // Get colors from CSS variables
          const getCSSVar = (name) => {
            return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
          };
          
          const colors = {
            speed: getCSSVar('--graph-speed'),
            heartrate: getCSSVar('--graph-heartrate'),
            height: getCSSVar('--graph-height'),
            cadence: getCSSVar('--graph-cadence')
          };
          
          let yAxisIDLeft = 'y';
          let yAxisIDRight = 'y1';
          
          // Helper function to create HR dataset with zone coloring (same as activity page)
          const createHRDataset = (metricData, yAxisID, label) => {
            const zoneColors = [
              getCSSVar('--graph-hr-zone1'),
              getCSSVar('--graph-hr-zone2'),
              getCSSVar('--graph-hr-zone3'),
              getCSSVar('--graph-hr-zone4'),
              getCSSVar('--graph-hr-zone5')
            ];
            
            // Create data points with zone information
            const dataPoints = metricData.map((p, idx) => {
              let xValue;
              if (xAxisType === 'distance' && p.distance != null) {
                xValue = p.distance / 1000; // Convert from meters to km
              } else {
                xValue = new Date(p.time).getTime();
              }
              return {
                x: xValue,
                y: p.value,
                zone: p.zone || null,
                time: p.time // Keep time for synchronization
              };
            });
            
            // Create a single dataset
            const dataset = {
              label: label,
              data: dataPoints,
              backgroundColor: 'transparent',
              yAxisID: yAxisID,
              borderWidth: 2,
              pointRadius: 0,
              pointHoverRadius: 4
            };
            
            // If zones are available, use segment coloring
            if (includeZones && metricData.some(p => p.zone)) {
              dataset.segment = {
                borderColor: (ctx) => {
                  // Color the segment based on the zone of the first point (p0)
                  if (!ctx || !ctx.p0 || !ctx.p0.raw) {
                    return colors.heartrate;
                  }
                  const point = ctx.p0.raw;
                  if (point && point.zone && point.zone >= 1 && point.zone <= 5) {
                    return zoneColors[point.zone - 1];
                  }
                  return colors.heartrate;
                }
              };
              // Set default border color (will be overridden by segment coloring)
              dataset.borderColor = colors.heartrate;
            } else {
              // No zones, use simple coloring
              dataset.borderColor = colors.heartrate;
            }
            
            return [dataset];
          };
          
          if (metric1 && data[metric1]) {
            const metricData = data[metric1];
            if (metric1 === 'heartrate') {
              datasets.push(...createHRDataset(metricData, yAxisIDLeft, 'HR'));
            } else {
              // Convert speed from m/s to km/h (multiply by 3.6)
              const convertValue = (value, metric) => {
                return metric === 'speed' ? value * 3.6 : value;
              };
              datasets.push({
                label: metric1.charAt(0).toUpperCase() + metric1.slice(1),
                data: metricData.map((p, idx) => {
                  let xValue;
                  if (xAxisType === 'distance' && p.distance != null) {
                    xValue = p.distance / 1000; // Convert from meters to km
                  } else {
                    xValue = new Date(p.time).getTime();
                  }
                  return { 
                    x: xValue, 
                    y: convertValue(p.value, metric1),
                    time: p.time // Keep time for synchronization
                  };
                }),
                borderColor: colors[metric1] || getCSSVar('--graph-speed'),
                backgroundColor: 'transparent',
                yAxisID: yAxisIDLeft,
                borderWidth: 2,
                pointRadius: 0,
                pointHoverRadius: 4
              });
            }
          }
          
          if (metric2 && data[metric2]) {
            const metricData = data[metric2];
            if (metric2 === 'heartrate') {
              datasets.push(...createHRDataset(metricData, metric1 ? yAxisIDRight : yAxisIDLeft, 'HR'));
            } else {
              // Convert speed from m/s to km/h (multiply by 3.6)
              const convertValue = (value, metric) => {
                return metric === 'speed' ? value * 3.6 : value;
              };
              datasets.push({
                label: metric2.charAt(0).toUpperCase() + metric2.slice(1),
                data: metricData.map((p, idx) => {
                  let xValue;
                  if (xAxisType === 'distance' && cumulativeDistances && segmentGraphPoints) {
                    const pointTime = new Date(p.time).getTime();
                    const matchingPoint = segmentGraphPoints.find(pt => Math.abs(new Date(pt.time).getTime() - pointTime) < 1000);
                    if (matchingPoint) {
                      const pointIdx = segmentGraphPoints.indexOf(matchingPoint);
                      xValue = cumulativeDistances[pointIdx] / 1000; // Convert to km
                    } else {
                      xValue = new Date(p.time).getTime(); // Fallback to time
                    }
                  } else {
                    xValue = new Date(p.time).getTime();
                  }
                  return { 
                    x: xValue, 
                    y: convertValue(p.value, metric2),
                    time: p.time // Keep time for synchronization
                  };
                }),
                borderColor: colors[metric2] || getCSSVar('--graph-cadence'),
                backgroundColor: 'transparent',
                yAxisID: metric1 ? yAxisIDRight : yAxisIDLeft,
                borderWidth: 2,
                pointRadius: 0,
                pointHoverRadius: 4
              });
            }
          }
          
          if (datasets.length === 0) return;
          
          // Destroy existing chart properly
          if (segmentChartInstance) {
            try {
              segmentChartInstance.destroy();
            } catch (e) {
              console.warn('Error destroying segment chart:', e);
            }
            segmentChartInstance = null;
          }
          
          // Create new chart (use setTimeout to ensure canvas is released)
          setTimeout(() => {
            try {
              const ctx = graphCanvas.getContext('2d');
              if (!ctx) {
                console.error('Failed to get canvas context');
                return;
              }
              
              if (typeof Chart === 'undefined') {
                console.error('Chart.js is not loaded');
                return;
              }
              
              segmentChartInstance = new Chart(ctx, {
                type: 'line',
                data: { datasets },
                options: {
                  responsive: true,
                  maintainAspectRatio: false,
                  interaction: {
                    intersect: false,
                    mode: 'index'
                  },
                  plugins: {
                    legend: {
                      display: true,
                      position: 'top',
                      labels: {
                        color: '#e0e0e0'
                      }
                    },
                    tooltip: {
                      callbacks: {
                        title: (context) => {
                          if (xAxisType === 'distance') {
                            const xValue = context[0].parsed.x;
                            return `Distance: ${xValue.toFixed(2)} km`;
                          } else {
                            // Time axis - use default time formatting
                            return context[0].label;
                          }
                        },
                        label: (context) => {
                          const label = context.dataset.label || '';
                          const value = context.parsed.y;
                          let unit = '';
                          if (label === 'Speed') unit = ' km/h';
                          else if (label === 'HR') unit = ' bpm';
                          else if (label === 'Height') unit = ' m';
                          else if (label === 'Cadence') unit = ' rpm';
                          return `${label}: ${value.toFixed(1)}${unit}`;
                        }
                      }
                    }
                  },
                  scales: {
                    x: xAxisType === 'distance' ? {
                      type: 'linear',
                      position: 'bottom',
                      title: {
                        display: true,
                        text: 'Distance (km)',
                        color: '#e0e0e0'
                      },
                      ticks: {
                        color: '#e0e0e0',
                        callback: function(value) {
                          return value.toFixed(1) + ' km';
                        }
                      },
                      grid: {
                        color: '#333'
                      }
                    } : {
                      type: 'time',
                      time: {
                        displayFormats: {
                          minute: 'HH:mm'
                        }
                      },
                      ticks: {
                        color: '#e0e0e0'
                      },
                      grid: {
                        color: '#333'
                      }
                    },
                    y: {
                      position: 'left',
                      ticks: {
                        color: '#e0e0e0'
                      },
                      grid: {
                        color: '#333'
                      }
                    },
                    y1: {
                      type: 'linear',
                      display: metric1 && metric2,
                      position: 'right',
                      ticks: {
                        color: '#e0e0e0'
                      },
                      grid: {
                        drawOnChartArea: false
                      }
                    }
                  }
                }
              });
            } catch (error) {
              console.error('Error creating segment chart:', error);
            }
          }, 10);
        })
        .catch(error => {
          console.error('Error loading segment graph:', error);
        });
    }

    if (metric1Select && metric2Select) {
      metric1Select.addEventListener('change', () => {
        if (selectedEfforts.size > 0) {
          updateSegmentComparisonGraph();
        }
      });
      metric2Select.addEventListener('change', () => {
        if (selectedEfforts.size > 0) {
          updateSegmentComparisonGraph();
        }
      });
      if (xAxisSelect) {
        xAxisSelect.addEventListener('change', () => {
          if (selectedEfforts.size > 0) {
            updateSegmentComparisonGraph();
          }
        });
      }
    }

    function applyColorMetric(features) {
      // Use stored features if available, otherwise use provided
      const featuresToUse = features || currentActivityFeatures;
      if (!featuresToUse) {
        console.warn('No features available for color metric');
        return;
      }
      const select = document.getElementById('color-metric');
      const legend = document.getElementById('legend');
      if (!select) {
        console.warn('Color metric select not found');
        return;
      }
      
      // Wait for map layer to be ready
      if (!map.getLayer('activity-points-layer')) {
        console.warn('Activity points layer not found, retrying...');
        setTimeout(() => applyColorMetric(features), 100);
        return;
      }

      const applyColor = async () => {
        const metric = select.value;
        try {
          if (metric === 'none') {
            map.setPaintProperty('activity-points-layer', 'circle-opacity', 0);
            if (map.getLayer('activity-segment-portion-line')) {
              map.setPaintProperty('activity-segment-portion-line', 'line-color', '#4cc9f0');
            }
            if (map.getLayer('activity-route-line')) {
              map.setPaintProperty('activity-route-line', 'line-color', '#4cc9f0');
            }
            if (legend) legend.style.display = 'none';
            return;
          }
          if (metric === 'moving') {
            map.setPaintProperty('activity-points-layer', 'circle-opacity', [
              'case',
              ['==', ['get', 'moving'], false],
              1,
              0
            ]);
            map.setPaintProperty('activity-points-layer', 'circle-color', '#e74c3c');
            const movingExpr = [
              'case',
              ['==', ['get', 'moving'], false],
              '#e74c3c',
              '#4cc9f0'
            ];
            if (map.getLayer('activity-segment-portion-line')) {
              map.setPaintProperty('activity-segment-portion-line', 'line-color', movingExpr);
            }
            if (map.getLayer('activity-route-line')) {
              map.setPaintProperty('activity-route-line', 'line-color', movingExpr);
            }
            if (legend) legend.style.display = 'none';
          } else if (metric === 'hrzones') {
            try {
              const zr = await fetch('/api/hrzones');
              if (!zr.ok) throw new Error('zones fetch failed');
              const z = await zr.json();
              const colors = ['#1b3a8a', '#00c2ff', '#2ecc71', '#f1c40f', '#e74c3c'];
              const zonesArr = (z && z.heart_rate && Array.isArray(z.heart_rate.zones)) ? z.heart_rate.zones : [];
              if (zonesArr.length === 0) {
                const {min, max} = computeRange(featuresToUse, 'heartrate');
                const gradExpr = gradientExpression('heartrate', min, max);
                map.setPaintProperty('activity-points-layer', 'circle-opacity', 1);
                map.setPaintProperty('activity-points-layer', 'circle-color', gradExpr);
                if (map.getLayer('activity-segment-portion-line')) {
                  map.setPaintProperty('activity-segment-portion-line', 'line-color', gradExpr);
                }
                if (map.getLayer('activity-route-line')) {
                  map.setPaintProperty('activity-route-line', 'line-color', gradExpr);
                }
                if (legend) renderGradientLegendVertical(legend, 'HR', min, max);
                return;
              }
              const zoneSteps = hrZonesExpression({heart_rate:{zones:zonesArr}}, colors);
              map.setPaintProperty('activity-points-layer', 'circle-opacity', 1);
              map.setPaintProperty('activity-points-layer', 'circle-color', zoneSteps);
              if (map.getLayer('activity-segment-portion-line')) {
                map.setPaintProperty('activity-segment-portion-line', 'line-color', zoneSteps);
              }
              if (map.getLayer('activity-route-line')) {
                map.setPaintProperty('activity-route-line', 'line-color', zoneSteps);
              }
              if (legend) renderZonesLegendVertical(legend, colors, zonesArr);
            } catch (e) {
              console.error('HR zones error', e);
            }
          } else {
            map.setPaintProperty('activity-points-layer', 'circle-opacity', 1);
            const {min, max} = computeRange(featuresToUse, metric);
            const gradExpr = gradientExpression(metric, min, max);
            map.setPaintProperty('activity-points-layer', 'circle-color', gradExpr);
            if (map.getLayer('activity-segment-portion-line')) {
              map.setPaintProperty('activity-segment-portion-line', 'line-color', gradExpr);
            }
            if (map.getLayer('activity-route-line')) {
              map.setPaintProperty('activity-route-line', 'line-color', gradExpr);
            }
            if (legend) renderGradientLegendVertical(legend, labelFor(metric), min, max);
          }
        } catch (e) {
          console.error('Error applying color metric:', e);
        }
      };
      
      // Don't clone/replace - just add listener if not already added
      if (!select._colorMetricListenerAdded) {
        select._colorMetricListenerAdded = true;
        select.addEventListener('change', () => {
          // Re-apply color when select changes
          applyColorMetric(currentActivityFeatures);
        });
      }
      applyColor();
    }

    function loadSegmentMetrics(activityID, segID, tolerance) {
      fetch(`/api/segments/${segID}/activity/${activityID}/metrics?tolerance=${tolerance}`)
        .then(r => {
          if (!r.ok) {
            throw new Error(`HTTP ${r.status}: ${r.statusText}`);
          }
          return r.json();
        })
        .then(metrics => {
          const metricsEl = document.getElementById(`segment-metrics-${activityID}`);
          if (!metricsEl) return;
          
          let html = '';
          if (metrics.avg_hr && metrics.avg_hr > 0) {
            html += `Avg HR: ${Math.round(metrics.avg_hr)} bpm`;
          }
          if (metrics.avg_speed && metrics.avg_speed > 0) {
            if (html) html += ' • ';
            html += `Avg Speed: ${(metrics.avg_speed * 3.6).toFixed(1)} km/h`;
          }
          if (metrics.distance && metrics.distance > 0) {
            if (html) html += ' • ';
            html += `Distance: ${(metrics.distance / 1000).toFixed(2)} km`;
          }
          if (metrics.elevation_gain && metrics.elevation_gain > 0) {
            if (html) html += ' • ';
            html += `Elevation: ${Math.round(metrics.elevation_gain)} m`;
          }
          
          metricsEl.innerHTML = html || '<span class="meta">No metrics available</span>';
        })
        .catch(err => {
          console.error(`Failed to load metrics for activity ${activityID}:`, err);
          const metricsEl = document.getElementById(`segment-metrics-${activityID}`);
          if (metricsEl) {
            metricsEl.innerHTML = `<span class="meta">Error: ${err.message}</span>`;
          }
        });
    }

    if (findBtn) {
      findBtn.addEventListener('click', () => loadActivities(false));
    }
    if (refreshBtn) {
      refreshBtn.addEventListener('click', () => loadActivities(true));
    }
    if (sortSelect) {
      sortSelect.addEventListener('change', () => {
        if (activitiesSection.style.display !== 'none') {
          loadActivities(false);
        }
      });
    }
    loadActivities(false);
  }

  function onDiscoveredPage() {
    const el = document.getElementById('discovered-map');
    if (!el) return;

    const mapStyleURL = window.__MAP_STYLE_URL__;
    if (!mapStyleURL) return;

    const statusEl = document.getElementById('discovered-status');
    const rebuildBtn = document.getElementById('discovered-rebuild-btn');
    const map = new maplibregl.Map({
      container: 'discovered-map',
      style: mapStyleURL,
      center: [0, 0],
      zoom: 2
    });
    installMissingStyleImageFallback(map);

    let hasFitCoverage = false;
    let fogRequestID = 0;

    const setStatus = (message, state = '') => {
      if (!statusEl) return;
      statusEl.textContent = message;
      statusEl.classList.toggle('warning', state === 'warning');
      statusEl.classList.toggle('ready', state === 'ready');
    };

    const loadStatus = async () => {
      const response = await fetch('/api/discovered/status');
      if (!response.ok) throw new Error(await response.text() || 'Failed to load discovered map status');
      const status = await response.json();

      if (status.stale) {
        setStatus(status.message || 'Discovered map is out of sync. Rebuild coverage to refresh the fog.', 'warning');
      } else {
        const rebuilt = status.rebuilt_at ? ` Last rebuilt ${new Date(status.rebuilt_at).toLocaleString()}.` : '';
        setStatus(`Coverage is up to date for ${status.cached_activities || 0} bike activities.${rebuilt}`, 'ready');
      }

      if (!hasFitCoverage && Array.isArray(status.bbox) && status.bbox.length === 4) {
        hasFitCoverage = true;
        map.fitBounds([[status.bbox[0], status.bbox[1]], [status.bbox[2], status.bbox[3]]], {
          padding: 60,
          duration: 0
        });
      }

      return status;
    };

    const fetchDiscoveredOverlay = async () => {
      if (!map.getSource('discovered-fog')) return;
      const requestID = ++fogRequestID;
      const bounds = expandedMapBounds(map, 2.5);
      const bbox = [
        bounds.minLng,
        bounds.minLat,
        bounds.maxLng,
        bounds.maxLat
      ].join(',');
      const [fogResponse, coverageResponse] = await Promise.all([
        fetch(`/api/discovered/fog?bbox=${encodeURIComponent(bbox)}`),
        fetch(`/api/discovered/coverage?bbox=${encodeURIComponent(bbox)}`)
      ]);
      if (!fogResponse.ok) throw new Error(await fogResponse.text() || 'Failed to load discovered fog');
      if (!coverageResponse.ok) throw new Error(await coverageResponse.text() || 'Failed to load discovered coverage');
      const fog = await fogResponse.json();
      const coverage = await coverageResponse.json();
      if (requestID !== fogRequestID) return;
      map.getSource('discovered-fog').setData(fog);
      if (map.getSource('discovered-coverage')) {
        map.getSource('discovered-coverage').setData(coverage);
      }
    };

    map.on('load', async () => {
      map.addSource('discovered-fog', {
        type: 'geojson',
        data: { type: 'FeatureCollection', features: [] }
      });
      map.addSource('discovered-coverage', {
        type: 'geojson',
        data: { type: 'FeatureCollection', features: [] }
      });
      map.addLayer({
        id: 'discovered-glow-outer',
        type: 'line',
        source: 'discovered-coverage',
        paint: {
          'line-color': '#70d6ff',
          'line-opacity': ['interpolate', ['linear'], ['zoom'], 6, 0.58, 10, 0.24, 13, 0],
          'line-width': ['interpolate', ['linear'], ['zoom'], 5, 20, 8, 34, 11, 24, 13, 0],
          'line-blur': ['interpolate', ['linear'], ['zoom'], 5, 18, 10, 12, 13, 0]
        }
      });
      map.addLayer({
        id: 'discovered-glow-inner',
        type: 'line',
        source: 'discovered-coverage',
        paint: {
          'line-color': '#ff7a59',
          'line-opacity': ['interpolate', ['linear'], ['zoom'], 6, 0.44, 10, 0.18, 13, 0],
          'line-width': ['interpolate', ['linear'], ['zoom'], 5, 8, 8, 16, 11, 10, 13, 0],
          'line-blur': ['interpolate', ['linear'], ['zoom'], 5, 8, 10, 5, 13, 0]
        }
      });
      map.addLayer({
        id: 'discovered-fog',
        type: 'fill',
        source: 'discovered-fog',
        paint: {
          'fill-color': '#05070a',
          'fill-opacity': 0.9
        }
      });

      try {
        await loadStatus();
        await fetchDiscoveredOverlay();
      } catch (error) {
        setStatus(error.message, 'warning');
      }
    });

    map.on('moveend', () => {
      fetchDiscoveredOverlay().catch(error => setStatus(error.message, 'warning'));
    });

    if (rebuildBtn) {
      rebuildBtn.addEventListener('click', async () => {
        rebuildBtn.disabled = true;
        setStatus('Rebuilding discovered coverage...', 'warning');
        try {
          const response = await fetch('/api/discovered/rebuild', { method: 'POST' });
          if (!response.ok) throw new Error(await response.text() || 'Failed to rebuild discovered map');
          hasFitCoverage = false;
          await loadStatus();
          await fetchDiscoveredOverlay();
        } catch (error) {
          setStatus(error.message, 'warning');
        } finally {
          rebuildBtn.disabled = false;
        }
      });
    }
  }

  function expandedMapBounds(map, factor) {
    const bounds = map.getBounds();
    const west = bounds.getWest();
    const east = bounds.getEast();
    const south = bounds.getSouth();
    const north = bounds.getNorth();
    const lngPad = Math.max(0, east - west) * factor;
    const latPad = Math.max(0, north - south) * factor;
    return {
      minLng: Math.max(-180, west - lngPad),
      minLat: Math.max(-85, south - latPad),
      maxLng: Math.min(180, east + lngPad),
      maxLat: Math.min(85, north + latPad)
    };
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => { onActivityPage(); onIndexPage(); onSegmentsPage(); onSegmentPage(); onDiscoveredPage(); });
  } else {
    onActivityPage(); onIndexPage(); onSegmentsPage(); onSegmentPage(); onDiscoveredPage();
  }
})();
