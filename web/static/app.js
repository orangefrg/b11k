(() => {
  function onActivityPage() {
    const mapToken = window.__MAPBOX_TOKEN__;
    if (!mapToken) return;
    const m = location.pathname.match(/\/activity\/(\d+)/);
    if (!m) return;
    const id = m[1];
    mapboxgl.accessToken = mapToken;
    const map = new mapboxgl.Map({
      container: 'map',
      style: 'mapbox://styles/mapbox/dark-v11',
      center: [0,0],
      zoom: 2
    });
    fetch('/api/activities/' + id + '/points').then(r=>r.json()).then(points => {
      if (!Array.isArray(points) || points.length===0) return;
      const lineCoords = points.map(p => [p.lng, p.lat]);
      const features = points.map((p, idx) => ({
        type: 'Feature',
        geometry: { type: 'Point', coordinates: [p.lng, p.lat] },
        properties: { idx, time: p.time, speed: p.speed, cadence: p.cadence, heartrate: p.heartrate, alt: p.altitude, grade: p.grade, moving: p.moving }
      }));
      const fc = { type: 'FeatureCollection', features };

      map.on('load', () => {
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
              // Store metric values at both endpoints for gradient (average of start and end)
              speed: ((features[i].properties?.speed || 0) + (features[i + 1]?.properties?.speed || 0)) / 2,
              heartrate: ((features[i].properties?.heartrate || 0) + (features[i + 1]?.properties?.heartrate || 0)) / 2,
              cadence: ((features[i].properties?.cadence || 0) + (features[i + 1]?.properties?.cadence || 0)) / 2,
              alt: ((features[i].properties?.alt || 0) + (features[i + 1]?.properties?.alt || 0)) / 2,
              grade: ((features[i].properties?.grade || 0) + (features[i + 1]?.properties?.grade || 0)) / 2,
              moving: features[i].properties?.moving || false
            }
          });
        }
        
        try {
          map.addSource('route', { type: 'geojson', data: { type: 'FeatureCollection', features: lineSegments } });
          map.addLayer({ id: 'route-line', type: 'line', source: 'route', paint: { 'line-color': '#4cc9f0', 'line-width': 3 } });
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
            source: 'route',
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

        // Start/Finish markers
        try {
          if (!map.hasImage('marker-start')) {
            const s = 28; const c = document.createElement('canvas'); c.width = s; c.height = s; const g = c.getContext('2d');
            g.fillStyle = '#2ecc71'; g.beginPath(); g.arc(s/2, s/2, s*0.35, 0, Math.PI*2); g.fill();
            g.fillStyle = '#0b1020'; g.font = 'bold 14px sans-serif'; g.textAlign='center'; g.textBaseline='middle'; g.fillText('S', s/2, s/2);
            map.addImage('marker-start', g.getImageData(0,0,s,s));
          }
          if (!map.hasImage('marker-finish')) {
            const s = 28; const c = document.createElement('canvas'); c.width = s; c.height = s; const g = c.getContext('2d');
            g.fillStyle = '#e74c3c'; g.beginPath(); g.arc(s/2, s/2, s*0.35, 0, Math.PI*2); g.fill();
            g.fillStyle = '#0b1020'; g.font = 'bold 14px sans-serif'; g.textAlign='center'; g.textBaseline='middle'; g.fillText('F', s/2, s/2);
            map.addImage('marker-finish', g.getImageData(0,0,s,s));
          }
          map.addSource('start-finish', {
            type: 'geojson',
            data: {
              type: 'FeatureCollection',
              features: [
                { type: 'Feature', geometry: { type: 'Point', coordinates: lineCoords[0] }, properties: { type: 'start' } },
                { type: 'Feature', geometry: { type: 'Point', coordinates: lineCoords[lineCoords.length-1] }, properties: { type: 'finish' } }
              ]
            }
          });
          map.addLayer({ id: 'start-marker', type: 'symbol', source: 'start-finish', layout: { 'icon-image': ['case', ['==',['get','type'],'start'], 'marker-start', 'marker-start'], 'icon-size': 1, 'icon-allow-overlap': true }, filter: ['==',['get','type'],'start'] });
          map.addLayer({ id: 'finish-marker', type: 'symbol', source: 'start-finish', layout: { 'icon-image': ['case', ['==',['get','type'],'finish'], 'marker-finish', 'marker-finish'], 'icon-size': 1, 'icon-allow-overlap': true }, filter: ['==',['get','type'],'finish'] });
        } catch (e) {
          console.warn('Error adding start/finish markers:', e);
        }

        try {
          map.addSource('route-points', { type: 'geojson', data: fc });
          map.addLayer({ id: 'route-points-layer', type: 'circle', source: 'route-points', paint: { 'circle-radius': 3, 'circle-color': '#f72585' } });
        } catch (e) {
          console.warn('Error adding route points:', e);
        }

        const bounds = new mapboxgl.LngLatBounds();
        for (const c of lineCoords) bounds.extend(c);
        if (!bounds.isEmpty()) map.fitBounds(bounds, { padding: 40, duration: 0 });

        const popup = new mapboxgl.Popup({ closeButton: true, closeOnClick: true, className: 'point-popup' });
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
          const applyColor = async () => {
            const metric = select.value;
            try {
              if (metric === 'none') {
                // Show points if in segment creation mode, otherwise hide them
                const opacity = segmentCreationMode ? 1 : 0;
                map.setPaintProperty('route-points-layer', 'circle-opacity', opacity);
                map.setPaintProperty('route-points-layer', 'circle-color', '#f72585');
                map.setPaintProperty('route-points-layer', 'circle-radius', 3);
                map.setPaintProperty('route-line', 'line-color', '#4cc9f0');
                if (legend) legend.style.display = 'none';
                return;
              }
              if (metric === 'moving') {
                map.setPaintProperty('route-points-layer', 'circle-opacity', [
                  'case',
                  ['==', ['get', 'moving'], false],
                  1,
                  0
                ]);
                map.setPaintProperty('route-points-layer', 'circle-color', '#e74c3c');
                map.setPaintProperty('route-line', 'line-color', [
                  'case',
                  ['==', ['get', 'moving'], false],
                  '#e74c3c',
                  '#4cc9f0'
                ]);
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
                  map.setPaintProperty('route-line', 'line-color', gradientExpression('heartrate', min, max));
                  if (legend) renderGradientLegendVertical(legend, 'HR', min, max);
                  return;
                }
                const zoneSteps = hrZonesExpression({heart_rate:{zones:zonesArr}}, colors);
                map.setPaintProperty('route-points-layer', 'circle-opacity', 1);
                map.setPaintProperty('route-points-layer', 'circle-color', zoneSteps);
                map.setPaintProperty('route-line', 'line-color', zoneSteps);
                if (legend) renderZonesLegendVertical(legend, colors, zonesArr);
              } catch (e) {
                console.error('HR zones error', e);
              }
              } else {
                map.setPaintProperty('route-points-layer', 'circle-opacity', 1);
                const {min, max} = computeRange(features, metric);
                const gradExpr = gradientExpression(metric, min, max);
                map.setPaintProperty('route-points-layer', 'circle-color', gradExpr);
                map.setPaintProperty('route-line', 'line-color', gradExpr);
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
        let segmentPreviewLayer = null;
        const createSegmentBtn = document.getElementById('create-segment-btn');
        const segmentModal = document.getElementById('segment-modal');
        const segmentForm = document.getElementById('segment-form');
        const segmentCancelBtn = document.getElementById('segment-cancel-btn');
        const segmentSelectionInfo = document.getElementById('segment-selection-info');

        if (createSegmentBtn && segmentModal && segmentForm) {
          // Toggle segment creation mode
          createSegmentBtn.addEventListener('click', () => {
            segmentCreationMode = !segmentCreationMode;
            createSegmentBtn.textContent = segmentCreationMode ? 'Cancel Segment' : 'Create Segment';
            createSegmentBtn.style.background = segmentCreationMode ? '#e74c3c' : '';
            selectedPoints = [];
            
            // Remove preview and highlight layers
            if (segmentPreviewLayer && map.getLayer('segment-preview')) {
              map.removeLayer('segment-preview');
              map.removeSource('segment-preview');
            }
            if (map.getLayer('segment-first-point')) {
              map.removeLayer('segment-first-point');
              map.removeSource('segment-first-point');
            }
            
            if (segmentCreationMode) {
              map.getCanvas().style.cursor = 'crosshair';
              segmentSelectionInfo.textContent = 'Click two points on the map to select a segment.';
              
              // Show points even if color metric is "none"
              const currentMetric = select ? select.value : 'none';
              if (currentMetric === 'none') {
                map.setPaintProperty('route-points-layer', 'circle-opacity', 1);
                map.setPaintProperty('route-points-layer', 'circle-color', '#f72585');
                map.setPaintProperty('route-points-layer', 'circle-radius', 3);
              }
            } else {
              map.getCanvas().style.cursor = '';
              segmentModal.style.display = 'none';
              
              // Restore original color metric state
              if (select) {
                const applyColor = select._applyColor;
                if (applyColor) applyColor();
              }
            }
          });

          // Handle point selection
          const originalClickHandler = map.on('click', 'route-points-layer', (e) => {
            if (!segmentCreationMode) return;
            const f = e.features && e.features[0];
            if (!f) return;
            const idx = f.properties.idx;
            
            if (selectedPoints.length === 0) {
              selectedPoints = [idx];
              segmentSelectionInfo.textContent = `First point selected (index ${idx}). Click another point.`;
              
              // Highlight the first selected point
              const firstPoint = features[idx];
              if (firstPoint) {
                // Remove existing highlight if any
                if (map.getLayer('segment-first-point')) {
                  map.removeLayer('segment-first-point');
                  map.removeSource('segment-first-point');
                }
                
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
                segmentSelectionInfo.textContent = 'Please select a different point.';
                return;
              }
              // Ensure start < end
              selectedPoints = [Math.min(startIdx, endIdx), Math.max(startIdx, endIdx)];
              segmentSelectionInfo.textContent = `Segment selected: points ${selectedPoints[0]} to ${selectedPoints[1]}.`;

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
                  paint: { 'line-color': '#f1c40f', 'line-width': 4, 'line-opacity': 0.8 }
                });
              }

              // Show modal
              segmentModal.style.display = 'flex';
            }
          });

          // Cancel segment creation
          if (segmentCancelBtn) {
            segmentCancelBtn.addEventListener('click', () => {
              segmentModal.style.display = 'none';
              segmentCreationMode = false;
              createSegmentBtn.textContent = 'Create Segment';
              createSegmentBtn.style.background = '';
              selectedPoints = [];
              
              // Remove preview and highlight layers
              if (map.getLayer('segment-preview')) {
                map.removeLayer('segment-preview');
                map.removeSource('segment-preview');
              }
              if (map.getLayer('segment-first-point')) {
                map.removeLayer('segment-first-point');
                map.removeSource('segment-first-point');
              }
              
              map.getCanvas().style.cursor = '';
              
              // Restore original color metric state
              if (select) {
                const applyColor = select._applyColor;
                if (applyColor) applyColor();
              }
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
                alert(`Segment "${segment.name}" created successfully!`);
                
                // Reset form and close modal
                segmentForm.reset();
                segmentModal.style.display = 'none';
                segmentCreationMode = false;
                createSegmentBtn.textContent = 'Create Segment';
                createSegmentBtn.style.background = '';
                selectedPoints = [];
                
                // Remove preview and highlight layers
                if (map.getLayer('segment-preview')) {
                  map.removeLayer('segment-preview');
                  map.removeSource('segment-preview');
                }
                if (map.getLayer('segment-first-point')) {
                  map.removeLayer('segment-first-point');
                  map.removeSource('segment-first-point');
                }
                
                map.getCanvas().style.cursor = '';
                
                // Restore original color metric state
                if (select) {
                  const applyColor = select._applyColor;
                  if (applyColor) applyColor();
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
          console.log('Graph elements found, setting up graph functionality');
          const updateGraph = async () => {
            const metric1 = metric1Select.value;
            const metric2 = metric2Select.value;
            const xAxisType = xAxisSelect ? xAxisSelect.value : 'time';
            
            console.log('updateGraph called with metrics:', metric1, metric2, 'xAxis:', xAxisType);
            
            const placeholder = document.getElementById('graph-placeholder');
            
            if (!metric1 && !metric2) {
              if (chartInstance) {
                chartInstance.destroy();
                chartInstance = null;
              }
              if (graphCanvas) graphCanvas.style.display = 'none';
              if (placeholder) placeholder.style.display = 'block';
              // Keep container visible so users can select metrics
              return;
            }
            
            // Hide placeholder and show canvas
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
            
            console.log('Fetching graph data from:', url);
            
            try {
              const response = await fetch(url);
              if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`Failed to fetch graph data: ${response.status} ${errorText}`);
              }
              const data = await response.json();
              console.log('Graph data received:', data);
              
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
        }
      });
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
    const deleteButtons = document.querySelectorAll('.delete-segment-btn');
    const deleteModal = document.getElementById('delete-modal');
    const deleteCancelBtn = document.getElementById('delete-cancel-btn');
    const deleteConfirmBtn = document.getElementById('delete-confirm-btn');
    const deleteSegmentName = document.getElementById('delete-segment-name');
    let segmentToDelete = null;

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

          deleteModal.style.display = 'none';
          segmentToDelete = null;

          // If no segments left, show message
          const remainingSegments = document.querySelectorAll('.item[data-segment-id]');
          if (remainingSegments.length === 0) {
            const list = document.querySelector('.list');
            if (list) {
              list.innerHTML = '<div>No segments found. Create segments from activity pages.</div>';
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
    const mapToken = window.__MAPBOX_TOKEN__;
    const segmentID = window.__SEGMENT_ID__;
    if (!mapToken || !segmentID) return;
    
    const m = location.pathname.match(/\/segment\/(\d+)/);
    if (!m) return;
    
    mapboxgl.accessToken = mapToken;
    const map = new mapboxgl.Map({
      container: 'map',
      style: 'mapbox://styles/mapbox/dark-v11',
      center: [0, 0],
      zoom: 2
    });

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
        const bounds = new mapboxgl.LngLatBounds();
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

    function loadActivities(forceRefresh = false) {
      const tolerance = parseFloat(toleranceInput.value) || 15;
      const sortBy = sortSelect.value || 'distance';
      const refreshParam = forceRefresh ? '&refresh=true' : '';

      activitiesLoading.style.display = 'block';
      activitiesSection.style.display = 'none';

      fetch(`/api/segments/${segmentID}/activities?tolerance=${tolerance}&sort=${sortBy}${refreshParam}`)
        .then(r => r.json())
        .then(activities => {
          activitiesLoading.style.display = 'none';
          activitiesSection.style.display = 'block';

          if (activities.length === 0) {
            activitiesList.innerHTML = '<div>No activities found matching this segment.</div>';
            return;
          }

          activitiesList.innerHTML = activities.map(activity => {
            // Format date
            let dateStr = 'Unknown date';
            if (activity.start_date_formatted) {
              try {
                dateStr = new Date(activity.start_date_formatted).toLocaleString();
              } catch (e) {
                dateStr = activity.start_date_formatted;
              }
            } else if (activity.start_date) {
              try {
                dateStr = new Date(activity.start_date).toLocaleString();
              } catch (e) {
                dateStr = activity.start_date;
              }
            }
            
            return `
            <div class="item" data-activity-id="${activity.id}" style="cursor: pointer; padding: 8px; margin-bottom: 8px; border: 1px solid var(--border); border-radius: 4px; ${selectedActivityID === activity.id ? 'background: var(--panel); border-color: #4cc9f0;' : ''}">
              <div style="display: flex; justify-content: space-between; align-items: center;">
                <div style="flex: 1;">
                  <div style="font-weight: 500;">${activity.name}</div>
                  <div class="meta">${dateStr}</div>
                  <div class="meta">Match: ${activity.min_distance_m.toFixed(1)}m distance, ${activity.overlap_percentage.toFixed(1)}% overlap</div>
                  <div class="meta" id="segment-metrics-${activity.id}" style="margin-top: 4px;">
                    <span style="color: #4cc9f0;">Loading segment metrics...</span>
                  </div>
                </div>
                <a class="link" href="/activity/${activity.id}" style="margin-left: 12px; white-space: nowrap;" onclick="event.stopPropagation();">View Full</a>
              </div>
            </div>
          `;
          }).join('');

          // Add click handlers
          activitiesList.querySelectorAll('.item[data-activity-id]').forEach(item => {
            item.addEventListener('click', (e) => {
              // Don't navigate if clicking on the "View Full" link
              if (e.target.tagName === 'A') return;
              
              const activityID = parseInt(item.getAttribute('data-activity-id'));
              selectedActivityID = activityID;
              
              // Update selected style
              activitiesList.querySelectorAll('.item').forEach(i => {
                i.style.background = '';
                i.style.borderColor = 'var(--border)';
              });
              item.style.background = 'var(--panel)';
              item.style.borderColor = '#4cc9f0';

              // Load and display activity points with segment portion highlighted
              // Preserve color metric selection
              const currentColorMetric = document.getElementById('color-metric')?.value || 'none';
              loadActivityPoints(activityID, segmentID, tolerance, currentColorMetric);
            });
          });
          
          // Load segment metrics for all activities with delay to avoid race conditions
          activities.forEach((activity, index) => {
            setTimeout(() => {
              loadSegmentMetrics(activity.id, segmentID, tolerance);
            }, index * 50); // Stagger requests by 50ms each
          });
        })
        .catch(err => {
          activitiesLoading.style.display = 'none';
          activitiesList.innerHTML = `<div style="color: #e74c3c;">Error loading activities: ${err.message}</div>`;
        });
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
              const popup = new mapboxgl.Popup({ closeButton: true, closeOnClick: true, className: 'point-popup' });
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
              const popup = new mapboxgl.Popup({ closeButton: true, closeOnClick: true, className: 'point-popup' });
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
        if (graphCanvas) graphCanvas.style.display = 'none';
        if (placeholder) placeholder.style.display = 'block';
        // Keep container visible so users can select metrics
        return;
      }
      
      // Hide placeholder and show canvas
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
        if (selectedActivityID) {
          updateSegmentGraph(selectedActivityID, segmentID);
        }
      });
      metric2Select.addEventListener('change', () => {
        if (selectedActivityID) {
          updateSegmentGraph(selectedActivityID, segmentID);
        }
      });
      if (xAxisSelect) {
        xAxisSelect.addEventListener('change', () => {
          if (selectedActivityID) {
            updateSegmentGraph(selectedActivityID, segmentID);
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
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => { onActivityPage(); onIndexPage(); onSegmentsPage(); onSegmentPage(); });
  } else {
    onActivityPage(); onIndexPage(); onSegmentsPage(); onSegmentPage();
  }
})();


