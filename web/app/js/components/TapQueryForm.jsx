import {
  defaultMaxRps,
  httpMethods,
  tapQueryPropType,
  tapResourceTypes
} from './util/TapUtils.jsx';
import Button from '@material-ui/core/Button';
import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import ExpandMoreIcon from '@material-ui/icons/ExpandMore';
import ExpansionPanel from '@material-ui/core/ExpansionPanel';
import ExpansionPanelDetails from '@material-ui/core/ExpansionPanelDetails';
import ExpansionPanelSummary from '@material-ui/core/ExpansionPanelSummary';
import FormControl from '@material-ui/core/FormControl';
import FormHelperText from '@material-ui/core/FormHelperText';
import Grid from '@material-ui/core/Grid';
import InputLabel from '@material-ui/core/InputLabel';
import MenuItem from '@material-ui/core/MenuItem';
import PropTypes from 'prop-types';
import QueryToCliCmd from './QueryToCliCmd.jsx';
import React from 'react';
import Select from '@material-ui/core/Select';
import TextField from '@material-ui/core/TextField';
import Typography from '@material-ui/core/Typography';
import _flatten from 'lodash/flatten';
import _isEmpty from 'lodash/isEmpty';
import _isEqual from 'lodash/isEqual';
import _isNil from 'lodash/isNil';
import _map from 'lodash/map';
import _noop from 'lodash/noop';
import _startCase from 'lodash/startCase';
import _uniq from 'lodash/uniq';
import _values from 'lodash/values';
import { withStyles } from '@material-ui/core/styles';
import { withTranslation } from 'react-i18next';


const getResourceList = (resourcesByNs, ns) => {
  return resourcesByNs[ns] || _uniq(_flatten(_values(resourcesByNs)));
};

const styles = theme => ({
  root: {
    display: 'flex',
    flexWrap: 'wrap',
  },
  formControl: {
    margin: theme.spacing.unit,
    minWidth: 200,
  },
  selectEmpty: {
    'margin-top': '32px'
  },
  card: {
    maxWidth: "100%",
  },
  actions: {
    display: 'flex',
    'padding-left': '32px'
  },
  expand: {
    transform: 'rotate(0deg)',
    transition: theme.transitions.create('transform', {
      duration: theme.transitions.duration.shortest,
    }),
    marginLeft: 'auto',
    [theme.breakpoints.up('sm')]: {
      marginRight: -8,
    },
  },
  expandOpen: {
    transform: 'rotate(180deg)',
  }
});


class TapQueryForm extends React.Component {
  static propTypes = {
    authoritiesByNs: PropTypes.shape({}),
    classes: PropTypes.shape({}).isRequired,
    cmdName: PropTypes.string.isRequired,
    enableAdvancedForm: PropTypes.bool,
    handleTapClear: PropTypes.func,
    handleTapStart: PropTypes.func.isRequired,
    handleTapStop: PropTypes.func.isRequired,
    query: tapQueryPropType.isRequired,
    t: PropTypes.func.isRequired,
    tapIsClosing: PropTypes.bool,
    tapRequestInProgress: PropTypes.bool.isRequired,
    updateQuery: PropTypes.func.isRequired,
  }

  static defaultProps = {
    authoritiesByNs: {},
    enableAdvancedForm: true,
    handleTapClear: _noop,
    tapIsClosing: false
  }

  static getDerivedStateFromProps(props, state) {
    if (!_isEqual(props.resourcesByNs, state.resourcesByNs)) {
      let resourcesByNs = props.resourcesByNs;
      let authoritiesByNs = props.authoritiesByNs;
      let namespaces = Object.keys(resourcesByNs).sort();
      let resourceNames  = getResourceList(resourcesByNs, props.query.namespace);
      let toResourceNames = getResourceList(resourcesByNs, props.query.toNamespace);
      let authorities = getResourceList(authoritiesByNs, props.query.namespace);

      return {
        ...state,
        resourcesByNs,
        authoritiesByNs,
        autocomplete: {
          namespace: namespaces,
          resource: resourceNames,
          toNamespace: namespaces,
          toResource: toResourceNames,
          authority: authorities
        }
      };
    } else {
      return null;
    }
  }

  constructor(props) {
    super(props);

    this.state = {
      authoritiesByNs: {},
      resourcesByNs: {},
      autocomplete: {
        namespace: [],
        resource: [],
        toNamespace: [],
        toResource: [],
        authority: []
      },
    };
  }

  handleFormChange = (name, scopeResource) => {
    let state = {
      autocomplete: this.state.autocomplete
    };
    let query = {};

    let shouldScopeAuthority = name === "namespace";

    return event => {
      let formVal = event.target.value;
      query[name] = formVal;

      if (!_isNil(scopeResource)) {
        // scope the available typeahead resources to the selected namespace
        state.autocomplete[scopeResource] = this.state.resourcesByNs[formVal];
        query[scopeResource] = `namespace/${formVal}`;
      }

      if (shouldScopeAuthority) {
        state.autocomplete.authority = this.state.authoritiesByNs[formVal];
      }

      this.props.updateQuery(query);
      this.setState(state);
    };
  }

  handleFormEvent = name => {
    return event => {
      this.props.updateQuery({ [name]: event.target.value });
    };
  }

  handleAdvancedFormExpandClick = () => {
    this.setState(state => ({advancedFormExpanded: !state.advancedFormExpanded}));
  }

  autoCompleteData = name => {
    return _uniq(
      this.state.autocomplete[name].filter(d => d.indexOf(this.props.query[name]) !== -1)
    ).sort();
  }

  resetTapForm = () => {
    this.props.handleTapClear();
  }

  renderResourceSelect = (resourceKey, namespaceKey) => {
    let selectedNs = this.props.query[namespaceKey];
    let nsEmpty = _isNil(selectedNs) || _isEmpty(selectedNs);
    let { classes } = this.props;

    let resourceOptions = tapResourceTypes.concat(
      this.state.autocomplete[resourceKey] || [],
      nsEmpty ? [] : [`namespace/${selectedNs}`]
    ).sort();

    return (
      <React.Fragment>
        <InputLabel htmlFor={resourceKey}>{this.props.t(_startCase(resourceKey))}</InputLabel>
        <Select
          value={nsEmpty ? _startCase(resourceKey) : this.props.query[resourceKey]}
          onChange={this.handleFormChange(resourceKey)}
          inputProps={{ name: resourceKey, id: resourceKey }}
          className={classes.selectEmpty}>
          {
            resourceOptions.map(resource => (
              <MenuItem key={`${namespaceKey}-${resourceKey}-${resource}`} value={resource}>{resource}</MenuItem>
            ))
          }
        </Select>
      </React.Fragment>
    );
  }

  renderNamespaceSelect = (title, namespaceKey, resourceKey) => {
    let { classes } = this.props;
    return (
      <React.Fragment>
        <InputLabel htmlFor={namespaceKey}>{this.props.t(title)}</InputLabel>
        <Select
          value={this.props.query[namespaceKey]}
          onChange={this.handleFormChange(namespaceKey, resourceKey)}
          inputProps={{ name: namespaceKey, id: namespaceKey }}
          className={classes.selectEmpty}>
          {
            _map(this.state.autocomplete[namespaceKey], (n, i) => (
              <MenuItem key={`ns-dr-${i}`} value={n}>{n}</MenuItem>
            ))
          }
        </Select>
      </React.Fragment>
    );
  }

  renderTapButton = (tapInProgress, tapIsClosing) => {
    if (tapIsClosing) {
      return (<Button variant="outlined" color="primary" className="tap-ctrl tap-stop" disabled={true}>{this.props.t("Stop")}</Button>);
    } else if (tapInProgress) {
      return (<Button variant="outlined" color="primary" className="tap-ctrl tap-stop" onClick={this.props.handleTapStop}>{this.props.t("Stop")}</Button>);
    } else {
      return (
        <Button
          color="primary"
          variant="outlined"
          className="tap-ctrl tap-start"
          disabled={!this.props.query.namespace || !this.props.query.resource}
          onClick={this.props.handleTapStart}>
          {this.props.t("Start")}
        </Button>);
    }
  }

  renderTextInput = (title, key, helperText) => {
    let { classes } = this.props;
    return (
      <TextField
        id={key}
        label={this.props.t(title)}
        className={classes.formControl}
        value={this.props.query[key]}
        onChange={this.handleFormEvent(key)}
        helperText={helperText}
        margin="normal" />
    );
  }

  renderAdvancedTapFormContent() {
    const { classes, query } = this.props;

    return (
      <Grid container>

        <Grid container spacing={24}>
          <Grid item xs={6} md={3}>
            <FormControl className={classes.formControl}>
              {this.renderNamespaceSelect("To Namespace", "toNamespace", "toResource")}
            </FormControl>
          </Grid>
          <Grid item xs={6} md={3}>
            <FormControl className={classes.formControl} disabled={_isEmpty(this.props.query.toNamespace)}>
              {this.renderResourceSelect("toResource", "toNamespace")}
            </FormControl>
          </Grid>
        </Grid>

        <Grid container spacing={24}>
          <Grid item xs={6} md={3}>
            <FormControl className={classes.formControl}>
              <InputLabel htmlFor="authority">Authority</InputLabel>
              <Select
                value={query.authority}
                onChange={this.handleFormChange("authority")}
                inputProps={{ name: 'authority', id: 'authority' }}
                className={classes.selectEmpty}>
                {
                _map(this.state.autocomplete.authority, (d, i) => (
                  <MenuItem key={`authority-${i}`} value={d}>{d}</MenuItem>
                ))
              }
              </Select>
              <FormHelperText>
                {this.props.t("Display requests with this :authority")}
              </FormHelperText>
            </FormControl>
          </Grid>
          <Grid item xs={6} md={3}>
            { this.renderTextInput("Path", "path", this.props.t("Display requests with paths that start with this prefix")) }
          </Grid>
        </Grid>

        <Grid container spacing={24}>
          <Grid item xs={6} md={3}>
            { this.renderTextInput("Scheme", "scheme", this.props.t("Display requests with this scheme")) }
          </Grid>
          <Grid item xs={6} md={3}>
            { this.renderTextInput("Max RPS", "maxRps", this.props.t("message1", { defaultMaxRps: defaultMaxRps })) }
          </Grid>
          <Grid item xs={6} md={3}>
            <FormControl className={classes.formControl}>
              <InputLabel htmlFor="method">{this.props.t("HTTP method")}</InputLabel>
              <Select
                value={this.props.query.method}
                onChange={this.handleFormChange("method")}
                inputProps={{ name: 'method', id: 'method' }}
                className={classes.selectEmpty}>
                {
                _map(httpMethods, d => (
                  <MenuItem key={`method-${d}`} value={d}>{d}</MenuItem>
                ))
              }
              </Select>
              <FormHelperText>
                {this.props.t("Display requests with this HTTP method")}
              </FormHelperText>
            </FormControl>
          </Grid>
        </Grid>

      </Grid>
    );
  }

  renderAdvancedTapForm() {
    return (
      <ExpansionPanel expanded={this.state.advancedFormExpanded} onChange={this.handleAdvancedFormExpandClick}>
        <ExpansionPanelSummary expandIcon={<ExpandMoreIcon />}>
          <Typography variant="caption" gutterBottom>
            {this.state.advancedFormExpanded ? this.props.t("Hide filters") : this.props.t("Show more filters")}
          </Typography>
        </ExpansionPanelSummary>

        <ExpansionPanelDetails>
          {this.renderAdvancedTapFormContent()}
        </ExpansionPanelDetails>
      </ExpansionPanel>
    );
  }

  render() {
    const { classes, query, t } = this.props;

    return (
      <Card className={classes.card}>
        <CardContent>
          <Grid container spacing={24}>
            <Grid item xs={6} md={3}>
              <FormControl className={classes.formControl}>
                {this.renderNamespaceSelect("Namespace", "namespace", "resource")}
              </FormControl>
            </Grid>

            <Grid item xs={6} md={3}>
              <FormControl className={classes.formControl} disabled={_isEmpty(query.namespace)}>
                {this.renderResourceSelect("resource", "namespace")}
              </FormControl>
            </Grid>

            <Grid item xs={4} md={1}>
              { this.renderTapButton(this.props.tapRequestInProgress, this.props.tapIsClosing) }
            </Grid>

            <Grid item xs={4} md={1}>
              <Button onClick={this.resetTapForm} disabled={this.props.tapRequestInProgress}>{t("Reset")}</Button>
            </Grid>
          </Grid>
        </CardContent>

        <QueryToCliCmd cmdName={this.props.cmdName} query={query} resource={query.resource} />

        { !this.props.enableAdvancedForm ? null : this.renderAdvancedTapForm() }

      </Card>
    );
  }
}

export default withTranslation(["tapQueryForm", "common"])(withStyles(styles)(TapQueryForm));
