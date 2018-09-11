import _ from 'lodash';
import ErrorBanner from './ErrorBanner.jsx';
import PageHeader from './PageHeader.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import TapEventTable from './TapEventTable.jsx';
import TapQueryCliCmd from './TapQueryCliCmd.jsx';
import TapQueryForm from './TapQueryForm.jsx';
import { withContext } from './util/AppContext.jsx';
import { httpMethods, processTapEvent, setMaxRps } from './util/TapUtils.jsx';
import './../../css/tap.css';

const maxNumFilterOptions = 12;
class Tap extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired,
    pathPrefix: PropTypes.string.isRequired
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.tapResultsById = {};
    this.tapResultFilterOptions = this.getInitialTapFilterOptions();
    this.debouncedWebsocketRecvHandler = _.throttle(this.updateTapResults, 500);
    this.loadFromServer = this.loadFromServer.bind(this);

    this.state = {
      tapResultsById: this.tapResultsById,
      tapResultFilterOptions: this.tapResultFilterOptions,
      error: null,
      resourcesByNs: {},
      authoritiesByNs: {},
      query: {
        resource: "",
        namespace: "",
        toResource: "",
        toNamespace: "",
        method: "",
        path: "",
        scheme: "",
        authority: "",
        maxRps: ""
      },
      maxLinesToDisplay: 40,
      tapRequestInProgress: false,
      pollingInterval: 10000,
      pendingRequests: false
    };
  }

  componentDidMount() {
    this.startServerPolling();
  }

  componentWillUnmount() {
    if (this.ws) {
      this.ws.close(1000);
    }
    this.debouncedWebsocketRecvHandler.cancel();
    this.stopTapStreaming();
    this.stopServerPolling();
  }

  onWebsocketOpen = () => {
    let query = _.cloneDeep(this.state.query);
    setMaxRps(query);

    this.ws.send(JSON.stringify({
      id: "tap-web",
      ...query
    }));
    this.setState({
      error: null
    });
  }

  onWebsocketRecv = e => {
    this.indexTapResult(e.data);
    this.debouncedWebsocketRecvHandler();
  }

  onWebsocketClose = e => {
    this.stopTapStreaming();

    if (!e.wasClean) {
      this.setState({
        error: {
          error: `Websocket [${e.code}] ${e.reason}`
        }
      });
    }
  }

  onWebsocketError = e => {
    this.setState({
      error: { error: e.message }
    });

    this.stopTapStreaming();
  }

  getInitialTapFilterOptions() {
    return {
      source: {},
      destination: {},
      path: {},
      authority: {},
      scheme: {},
      httpStatus: {},
      tls: {},
      httpMethod: httpMethods
    };
  }

  getResourcesByNs(rsp) {
    let statTables = _.get(rsp, [0, "ok", "statTables"]);
    let authoritiesByNs = {};
    let resourcesByNs = _.reduce(statTables, (mem, table) => {
      _.each(table.podGroup.rows, row => {
        // filter out resources that aren't meshed. note that authorities don't
        // have pod counts and therefore can't be filtered out here
        if (row.meshedPodCount === "0" && row.resource.type !== "authority") {
          return;
        }

        if (!mem[row.resource.namespace]) {
          mem[row.resource.namespace] = [];
          authoritiesByNs[row.resource.namespace] = [];
        }

        switch (row.resource.type.toLowerCase()) {
          case "service":
            break;
          case "authority":
            authoritiesByNs[row.resource.namespace].push(row.resource.name);
            break;
          default:
            mem[row.resource.namespace].push(`${row.resource.type}/${row.resource.name}`);
        }
      });
      return mem;
    }, {});
    return {
      authoritiesByNs,
      resourcesByNs
    };
  }

  getFilterOptions(d) {
    let filters = this.tapResultFilterOptions;
    // keep track of unique values we encounter, to populate the table filters
    let addFilter = this.genFilterAdder(filters, Date.now());
    addFilter("source", d.source.str);
    addFilter("destination", d.destination.str);
    if (d.source.pod) {
      addFilter("source", d.source.pod);
    }
    if (d.destination.pod) {
      addFilter("destination", d.destination.pod);
    }

    if (d.tls) {
      addFilter("tls", d.tls);
    }
    switch (d.eventType) {
      case "requestInit":
        addFilter("authority", d.http.requestInit.authority);
        addFilter("path", d.http.requestInit.path);
        addFilter("scheme", _.get(d, "http.requestInit.scheme.registered"));
        break;
      case "responseInit":
        addFilter("httpStatus", _.get(d, "http.responseInit.httpStatus"));
        break;
    }

    return filters;
  }

  parseTapResult = data => {
    let d = processTapEvent(data);

    return {
      tapResult: d,
      updatedFilters: this.getFilterOptions(d)
    };
  }

  genFilterAdder(filterOptions, now) {
    return (filterName, filterValue) => {
      filterOptions[filterName][filterValue] = now;

      if (_.size(filterOptions[filterName]) > maxNumFilterOptions) {
        // reevaluate this if table updating gets too slow
        let oldest = Date.now();
        let oldestOption = "";
        _.each(filterOptions[filterName], (timestamp, value) => {
          if (timestamp < oldest) {
            oldest = timestamp;
            oldestOption = value;
          }
        });

        delete filterOptions[filterName][oldestOption];
      }
    };
  }

  indexTapResult = data => {
    // keep an index of tap request rows by id. this allows us to collate
    // requestInit/responseInit/responseEnd into one single table row,
    // as opposed to three separate rows as in the CLI
    let resultIndex = this.tapResultsById;
    let parsedResults = this.parseTapResult(data);
    this.tapResultFilterOptions = parsedResults.updatedFilters;
    let d = parsedResults.tapResult;

    if (_.isNil(resultIndex[d.id])) {
      // don't let tapResultsById grow unbounded
      if (_.size(resultIndex) > this.state.maxLinesToDisplay) {
        this.deleteOldestTapResult(resultIndex);
      }

      resultIndex[d.id] = {};
    }
    resultIndex[d.id][d.eventType] = d;
    // assumption: requests of a given id all share the same high level metadata
    resultIndex[d.id]["base"] = d;
    resultIndex[d.id].lastUpdated = Date.now();
  }

  updateTapResults = () => {
    this.setState({
      tapResultsById: this.tapResultsById,
      tapResultFilterOptions: this.tapResultFilterOptions
    });
  }

  deleteOldestTapResult = resultIndex => {
    let oldest = Date.now();
    let oldestId = "";

    _.each(resultIndex, (res, id) => {
      if (res.lastUpdated < oldest) {
        oldest = res.lastUpdated;
        oldestId = id;
      }
    });

    delete resultIndex[oldestId];
  }

  startServerPolling() {
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  stopServerPolling() {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
  }

  startTapStreaming() {
    this.tapResultsById = {};
    this.tapResultFilterOptions = this.getInitialTapFilterOptions();

    this.setState({
      tapRequestInProgress: true,
      tapResultsById: this.tapResultsById,
      tapResultFilterOptions: this.tapResultFilterOptions
    });

    let protocol = window.location.protocol === "https:" ? "wss" : "ws";
    let tapWebSocket = `${protocol}://${window.location.host}${this.props.pathPrefix}/api/tap`;

    this.ws = new WebSocket(tapWebSocket);
    this.ws.onmessage = this.onWebsocketRecv;
    this.ws.onclose = this.onWebsocketClose;
    this.ws.onopen = this.onWebsocketOpen;
    this.ws.onerror = this.onWebsocketError;
  }

  stopTapStreaming() {
    this.setState({
      tapRequestInProgress: false
    });
  }

  handleTapStart = e => {
    e.preventDefault();
    this.startTapStreaming();
  }

  handleTapStop = () => {
    this.ws.close(1000);
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({
      pendingRequests: true
    });

    let url = this.api.urlsForResource("all");
    this.api.setCurrentRequests([this.api.fetchMetrics(url)]);
    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(rsp => {
        let { resourcesByNs, authoritiesByNs } = this.getResourcesByNs(rsp);

        this.setState({
          resourcesByNs,
          authoritiesByNs,
          pendingRequests: false
        });
      })
      .catch(this.handleApiError);
  }

  handleApiError = e => {
    if (e.isCanceled) {
      return;
    }

    this.setState({
      pendingRequests: false,
      error: e
    });
  }

  updateQuery = query => {
    this.setState({
      query
    });
  }

  render() {
    let tableRows = _(this.state.tapResultsById)
      .values().sortBy('lastUpdated').reverse().value();

    return (
      <div>
        {!this.state.error ? null :
        <ErrorBanner message={this.state.error} onHideMessage={() => this.setState({ error: null })} />}

        <PageHeader header="Tap" />
        <TapQueryForm
          tapRequestInProgress={this.state.tapRequestInProgress}
          handleTapStart={this.handleTapStart}
          handleTapStop={this.handleTapStop}
          resourcesByNs={this.state.resourcesByNs}
          authoritiesByNs={this.state.authoritiesByNs}
          updateQuery={this.updateQuery}
          query={this.state.query} />

        <TapQueryCliCmd cmdName="tap" query={this.state.query} />

        <TapEventTable
          tableRows={tableRows}
          filterOptions={this.state.tapResultFilterOptions} />
      </div>
    );
  }
}

export default withContext(Tap);
