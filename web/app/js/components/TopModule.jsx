import { WS_ABNORMAL_CLOSURE, WS_NORMAL_CLOSURE, processNeighborData, processTapEvent, setMaxRps, wsCloseCodes } from './util/TapUtils.jsx';

import ErrorBanner from './ErrorBanner.jsx';
import Percentage from './util/Percentage.js';
import PropTypes from 'prop-types';
import React from 'react';
import TopEventTable from './TopEventTable.jsx';
import _cloneDeep from 'lodash/cloneDeep';
import _each from 'lodash/each';
import _get from 'lodash/get';
import _has from 'lodash/has';
import _includes from 'lodash/includes';
import _isEmpty from 'lodash/isEmpty';
import _isEqual from 'lodash/isEqual';
import _isNil from 'lodash/isNil';
import _noop from 'lodash/noop';
import _size from 'lodash/size';
import _take from 'lodash/take';
import _throttle from 'lodash/throttle';
import _values from 'lodash/values';
import { withContext } from './util/AppContext.jsx';

// https://github.com/grpc/grpc/blob/master/doc/statuscodes.md
const grpcErrorStatusCodes = [2, 4, 13, 14, 15];

class TopModule extends React.Component {
  constructor(props) {
    super(props);
    this.tapResultsById = {};
    this.topEventIndex = {};
    this.throttledWebsocketRecvHandler = _throttle(this.updateTapEventIndexState, 500);
    this.updateTapClosingState = props.updateTapClosingState;
    this.unmeshedSources = {};

    this.state = {
      error: null,
      topEventIndex: {},
      showTapEnabledWarning: false,
    };
  }

  componentDidMount() {
    const { startTap } = this.props;
    if (startTap) {
      this.startTapStreaming();
    }
  }

  componentDidUpdate(prevProps) {
    const { startTap, query } = this.props;
    if (startTap && !prevProps.startTap) {
      this.startTapStreaming();
    }
    if (!startTap && prevProps.startTap) {
      this.stopTapStreaming();
    }
    if (!_isEqual(query, prevProps.query)) {
      this.clearTopTable();
    }
  }

  componentWillUnmount() {
    this.throttledWebsocketRecvHandler.cancel();
    this.stopTapStreaming();
    this.updateTapClosingState = _noop;
  }

  onWebsocketOpen = () => {
    const { query } = this.props;
    const tapQuery = _cloneDeep(query);
    setMaxRps(tapQuery);

    this.ws.send(JSON.stringify({
      id: 'top-web',
      ...tapQuery,
    }));
    this.setState({
      error: null,
    });
  }

  onWebsocketRecv = e => {
    this.indexTapResult(e.data);
    this.throttledWebsocketRecvHandler();
  }

  onWebsocketClose = e => {
    this.updateTapClosingState();
    /* We ignore any abnormal closure since it doesn't matter as long as
    the connection to the websocket is closed. This is also a workaround
    where Chrome browsers incorrectly displays a 1006 close code
    https://github.com/linkerd/linkerd2/issues/1630
    */
    if (e.code !== WS_NORMAL_CLOSURE && e.code !== WS_ABNORMAL_CLOSURE) {
      if (e.reason.includes('no pods to tap')) {
        this.setState({
          showTapEnabledWarning: true,
        });
      } else {
        this.setState({
          error: {
            error: `Websocket close error [${e.code}: ${wsCloseCodes[e.code]}] ${e.reason ? ':' : ''} ${e.reason}`,
          },
        });
      }
    }
  }

  onWebsocketError = e => {
    this.setState({
      error: { error: `Websocket error: ${e.message}` },
    });
  }

  closeWebSocket = () => {
    if (this.ws) {
      this.ws.close(1000);
    }
  }

  parseTapResult = data => {
    const d = processTapEvent(data);

    if (d.eventType === 'responseEnd') {
      d.latency = parseFloat(d.http.responseEnd.sinceRequestInit.replace('s', ''));
      d.completed = true;
    }

    return d;
  }

  topEventKey = event => {
    const sourceKey = event.source.owner || event.source.pod || event.source.str;
    const dstKey = event.destination.owner || event.destination.pod || event.destination.str;

    return [sourceKey, dstKey, _get(event, 'http.requestInit.method.registered'), event.http.requestInit.path].join('_');
  }

  initialTopResult = (d, eventKey) => {
    // in the event that we key on resources with multiple pods/ips, store them so we can display
    const sourceDisplay = {
      ips: {},
      pods: {},
    };
    sourceDisplay.ips[d.base.source.str] = true;
    if (!_isNil(d.base.source.pod)) {
      sourceDisplay.pods[d.base.source.pod] = d.base.source.namespace;
    }

    const destinationDisplay = {
      ips: {},
      pods: {},
    };
    destinationDisplay.ips[d.base.destination.str] = true;
    if (!_isNil(d.base.destination.pod)) {
      destinationDisplay.pods[d.base.destination.pod] = d.base.destination.namespace;
    }

    return {
      count: 1,
      best: d.responseEnd.latency,
      worst: d.responseEnd.latency,
      last: d.responseEnd.latency,
      success: !d.success ? 0 : 1,
      failure: !d.success ? 1 : 0,
      meshed: true,
      successRate: !d.success ? new Percentage(0, 1) : new Percentage(1, 1),
      direction: d.base.proxyDirection,
      source: d.requestInit.source,
      sourceLabels: d.requestInit.sourceMeta.labels,
      sourceDisplay,
      destination: d.requestInit.destination,
      destinationLabels: d.requestInit.destinationMeta.labels,
      destinationDisplay,
      httpMethod: _get(d, 'requestInit.http.requestInit.method.registered'),
      path: d.requestInit.http.requestInit.path,
      key: eventKey,
      lastUpdated: Date.now(),
    };
  }

  incrementTopResult = (d, result) => {
    result.count += 1;
    if (!d.success) {
      result.failure += 1;
    } else {
      result.success += 1;
    }
    result.successRate = new Percentage(result.success, result.success + result.failure);

    result.last = d.responseEnd.latency;
    if (d.responseEnd.latency < result.best) {
      result.best = d.responseEnd.latency;
    }
    if (d.responseEnd.latency > result.worst) {
      result.worst = d.responseEnd.latency;
    }

    result.sourceDisplay.ips[d.base.source.str] = true;
    if (!_isNil(d.requestInit.sourceMeta.labels.pod)) {
      result.sourceDisplay.pods[d.requestInit.sourceMeta.labels.pod] = d.requestInit.sourceMeta.labels.namespace;
    }
    result.destinationDisplay.ips[d.base.destination.str] = true;
    if (!_isNil(d.requestInit.destinationMeta.labels.pod)) {
      result.destinationDisplay.pods[d.requestInit.destinationMeta.labels.pod] = d.requestInit.destinationMeta.labels.namespace;
    }

    result.lastUpdated = Date.now();
  }

  indexTopResult = (d, topResults) => {
    const { query, maxRowsToStore } = this.props;

    // only index if have the full request (i.e. init and end)
    if (!d.requestInit) {
      return topResults;
    }

    const eventKey = this.topEventKey(d.requestInit);
    this.addSuccessCount(d);

    if (!topResults[eventKey]) {
      topResults[eventKey] = this.initialTopResult(d, eventKey);
    } else {
      this.incrementTopResult(d, topResults[eventKey]);
    }

    if (_size(topResults) > maxRowsToStore) {
      this.deleteOldestIndexedResult(topResults);
    }

    if (d.base.proxyDirection === 'INBOUND') {
      this.updateNeighborsFromTapData(d.requestInit.source, _get(d, 'requestInit.sourceMeta.labels'));
      const isPod = query.resource.split('/')[0] === 'pod';
      if (isPod) {
        topResults[eventKey].meshed = !_has(this.unmeshedSources, `pod/${d.base.source.pod}`);
      } else {
        topResults[eventKey].meshed = !_isEmpty(d.base.source.owner) && !_has(this.unmeshedSources, d.base.source.owner);
      }
    }

    return topResults;
  }

  updateTapEventIndexState = () => {
    // tap websocket events come in at a really high, bursty rate
    // calling setState every time an event comes in causes a lot of re-rendering
    // and causes the page to freeze. To fix this, limit the times we
    // update the state (and thus trigger a render)
    this.setState({
      topEventIndex: this.topEventIndex,
    });
  }

  updateNeighborsFromTapData = (source, sourceLabels) => {
    const { query, updateUnmeshedSources } = this.props;

    // store this outside of state, as updating the state upon every websocket event received
    // is very costly and causes the page to freeze up
    const resourceType = _isNil(query.resource) ? '' : query.resource.split('/')[0];
    this.unmeshedSources = processNeighborData(source, sourceLabels, this.unmeshedSources, resourceType);
    updateUnmeshedSources(this.unmeshedSources);
  }

  // keep an index of tap results by id until the request is complete.
  // when the request has completed, add it to the aggregated Top counts and
  // discard the individual tap result
  indexTapResult = data => {
    const { maxRowsToStore } = this.props;

    const resultIndex = this.tapResultsById;
    const d = this.parseTapResult(data);

    if (_isNil(resultIndex[d.id])) {
      // don't let tapResultsById grow unbounded
      if (_size(resultIndex) > maxRowsToStore) {
        this.deleteOldestIndexedResult(resultIndex);
      }

      resultIndex[d.id] = {};
    }
    resultIndex[d.id][d.eventType] = d;

    // assumption: requests of a given id all share the same high level metadata
    resultIndex[d.id].base = d;
    resultIndex[d.id].lastUpdated = Date.now();

    if (d.completed) {
      // only add results into top if the request has completed
      // we can also now delete this result from the Tap result index
      this.topEventIndex = this.indexTopResult(resultIndex[d.id], this.topEventIndex);
      delete resultIndex[d.id];
    }
  }

  addSuccessCount = d => {
    // cope with the fact that gRPC failures are returned with HTTP status 200
    // and correctly classify gRPC failures as failures
    let success = parseInt(_get(d, 'responseInit.http.responseInit.httpStatus'), 10) < 500;
    if (success) {
      const grpcStatusCode = _get(d, 'responseEnd.http.responseEnd.eos.grpcStatusCode');
      if (!_isNil(grpcStatusCode)) {
        success = !_includes(grpcErrorStatusCodes, grpcStatusCode);
      } else if (!_isNil(_get(d, 'responseEnd.http.responseEnd.eos.resetErrorCode'))) {
        success = false;
      }
    }

    d.success = success;
  }

  deleteOldestIndexedResult = resultIndex => {
    let oldest = Date.now();
    let oldestId = '';

    _each(resultIndex, (res, id) => {
      if (res.lastUpdated < oldest) {
        oldest = res.lastUpdated;
        oldestId = id;
      }
    });

    delete resultIndex[oldestId];
  }

  clearTopTable() {
    this.tapResultsById = {};
    this.topEventIndex = {};
    this.setState({
      topEventIndex: {},
      showTapEnabledWarning: false,
    });
  }

  startTapStreaming() {
    const { pathPrefix } = this.props;

    this.clearTopTable();

    const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
    const tapWebSocket = `${protocol}://${window.location.host}${pathPrefix}/api/tap`;

    this.ws = new WebSocket(tapWebSocket);
    this.ws.onmessage = this.onWebsocketRecv;
    this.ws.onclose = this.onWebsocketClose;
    this.ws.onopen = this.onWebsocketOpen;
    this.ws.onerror = this.onWebsocketError;
  }

  stopTapStreaming() {
    this.closeWebSocket();
  }

  banner = () => {
    const { error } = this.state;
    return error ? <ErrorBanner message={error} /> : null;
  }

  render() {
    const { topEventIndex, showTapEnabledWarning } = this.state;
    const { query, maxRowsToDisplay, tapEnabledWarningComponent } = this.props;

    const tableRows = _take(_values(topEventIndex), maxRowsToDisplay);
    const resourceType = _isNil(query.resource) ? '' : query.resource.split('/')[0];

    return (
      <React.Fragment>
        {this.banner()}
        {showTapEnabledWarning && tapEnabledWarningComponent}
        {!showTapEnabledWarning && <TopEventTable resourceType={resourceType} tableRows={tableRows} />}
      </React.Fragment>
    );
  }
}

TopModule.propTypes = {
  maxRowsToDisplay: PropTypes.number,
  maxRowsToStore: PropTypes.number,
  pathPrefix: PropTypes.string.isRequired,
  query: PropTypes.shape({
    resource: PropTypes.string,
    namespace: PropTypes.string,
  }),
  startTap: PropTypes.bool.isRequired,
  tapEnabledWarningComponent: PropTypes.node,
  updateTapClosingState: PropTypes.func,
  updateUnmeshedSources: PropTypes.func,
};

TopModule.defaultProps = {
  // max aggregated top rows to index and display in table
  maxRowsToDisplay: 40,
  // max rows to keep in index. there are two indexes we keep:
  // - un-ended tap results, pre-aggregation into the top counts
  // - aggregated top rows
  maxRowsToStore: 50,
  updateTapClosingState: _noop,
  updateUnmeshedSources: _noop,
  query: {
    resource: '',
    namespace: '',
  },
  tapEnabledWarningComponent: null,
};

export default withContext(TopModule);
